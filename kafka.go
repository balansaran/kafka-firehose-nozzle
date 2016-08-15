package main

import (
	"fmt"
	"log"
	"sync"
	"time"

	"golang.org/x/net/context"

	"github.com/Shopify/sarama"
	"github.com/cloudfoundry/sonde-go/events"
)

const (
	// TopicAppLogTmpl is Kafka topic name template for LogMessage
	TopicAppLogTmpl = "app-log-%s"

	// TopicCFMetrics is Kafka topic name for ValueMetric
	TopicCFMetric = "cf-metrics"
)

const (
	// Default topic name for each event
	DefaultContainerMetricTopic = "container-metric"
	DefaultValueMetricTopic     = "value-metric"
	DefaultLogMessageTopic      = "log-message"

	DefaultKafkaRetryMax     = 5
	DefaultKafkaRetryBackoff = 100 * time.Millisecond
)

func NewKafkaProducer(logger *log.Logger, stats *Stats, config *Config) (NozzleProducer, error) {
	// Setup kafka async producer (We must use sync producer)
	// TODO (tcnksm): Enable to configure more properties.
	producerConfig := sarama.NewConfig()

	producerConfig.Producer.Partitioner = sarama.NewRoundRobinPartitioner
	producerConfig.Producer.Return.Successes = true
	producerConfig.Producer.RequiredAcks = sarama.WaitForAll

	producerConfig.Producer.Retry.Max = DefaultKafkaRetryMax
	if config.Kafka.RetryMax != 0 {
		producerConfig.Producer.Retry.Max = config.Kafka.RetryMax
	}

	producerConfig.Producer.Retry.Backoff = DefaultKafkaRetryBackoff
	if config.Kafka.RetryBackoff != 0 {
		backoff := time.Duration(config.Kafka.RetryBackoff) * time.Millisecond
		producerConfig.Producer.Retry.Backoff = backoff
	}

	brokers := config.Kafka.Brokers
	if len(brokers) < 1 {
		return nil, fmt.Errorf("brokers are not provided")
	}

	asyncProducer, err := sarama.NewAsyncProducer(brokers, producerConfig)
	if err != nil {
		return nil, err
	}

	kafkaTopic := config.Kafka.Topic
	if kafkaTopic.LogMessage == "" {
		kafkaTopic.LogMessage = DefaultLogMessageTopic
	}

	if kafkaTopic.ValueMetric == "" {
		kafkaTopic.ValueMetric = DefaultValueMetricTopic
	}

	if kafkaTopic.ContainerMetric == "" {
		kafkaTopic.ContainerMetric = DefaultContainerMetricTopic
	}

	return &KafkaProducer{
		AsyncProducer:           asyncProducer,
		Logger:                  logger,
		Stats:                   stats,
		logMessageTopic:         kafkaTopic.LogMessage,
		logMessageTopicFmt:      kafkaTopic.LogMessageFmt,
		valueMetricTopic:        kafkaTopic.ValueMetric,
		containerMetricTopic:    kafkaTopic.ContainerMetric,
		containerMetricTopicFmt: kafkaTopic.ContainerMetricFmt,
	}, nil
}

// KafkaProducer implements NozzleProducer interfaces
type KafkaProducer struct {
	sarama.AsyncProducer

	logMessageTopic    string
	logMessageTopicFmt string

	valueMetricTopic string

	containerMetricTopic    string
	containerMetricTopicFmt string

	Logger *log.Logger
	Stats  *Stats

	once sync.Once
}

// init sets default logger
func (kp *KafkaProducer) init() {
	if kp.Logger == nil {
		kp.Logger = defaultLogger
	}
}

func (kp *KafkaProducer) LogMessageTopic(appID string) string {
	if kp.logMessageTopicFmt != "" {
		return fmt.Sprintf(kp.logMessageTopicFmt, appID)
	}

	return kp.logMessageTopic
}

func (kp *KafkaProducer) ValueMetricTopic() string {
	return kp.valueMetricTopic
}

func (kp *KafkaProducer) ContainerMetricTopic(appID string) string {
	if kp.containerMetricTopicFmt != "" {
		return fmt.Sprintf(kp.containerMetricTopicFmt, appID)
	}

	return kp.containerMetricTopic
}

// Produce produces event to kafka
func (kp *KafkaProducer) Produce(ctx context.Context, eventCh <-chan *events.Envelope) {
	kp.once.Do(kp.init)

	kp.Logger.Printf("[INFO] Start loop to watch events")
	for {
		select {
		case event, ok := <-eventCh:
			if !ok {
				kp.Logger.Printf("[ERROR] Nozzle consumer eventCh is closed")
				return
			}

			kp.input(event)

		case <-ctx.Done():
			// Stop process immediately
			kp.Logger.Printf("[INFO] Stop kafka producer")
			return
		}
	}
}

func (kp *KafkaProducer) input(event *events.Envelope) {
	switch eventType := event.GetEventType(); eventType {
	case events.Envelope_HttpStart:
		// Do nothing
	case events.Envelope_HttpStartStop:
		// Do nothing
	case events.Envelope_HttpStop:
		// Do nothing
	case events.Envelope_LogMessage:
		kp.Stats.Inc(Consume)
		appID := event.GetLogMessage().GetAppId()
		kp.Input() <- &sarama.ProducerMessage{
			Topic: kp.LogMessageTopic(appID),
			Value: &JsonEncoder{event: event},
		}
	case events.Envelope_ValueMetric:
		kp.Stats.Inc(Consume)
		kp.Input() <- &sarama.ProducerMessage{
			Topic: kp.ValueMetricTopic(),
			Value: &JsonEncoder{event: event},
		}
	case events.Envelope_CounterEvent:
		// Do nothing
	case events.Envelope_Error:
		// Do nothing
	case events.Envelope_ContainerMetric:
		kp.Stats.Inc(Consume)
		appID := event.GetContainerMetric().GetApplicationId()
		kp.Input() <- &sarama.ProducerMessage{
			Topic: kp.ContainerMetricTopic(appID),
			Value: &JsonEncoder{event: event},
		}
	}
}
