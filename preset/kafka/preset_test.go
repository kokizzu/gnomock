package kafka_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/orlangure/gnomock"
	"github.com/orlangure/gnomock/preset/kafka"
	kafkaclient "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/require"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestPreset(t *testing.T) {
	versions := []string{"3.3.1-L0", "3.6.1-L0"}

	for _, version := range versions {
		t.Run(version, testPreset(version))
	}
}

func testPreset(version string) func(t *testing.T) {
	return func(t *testing.T) {
		messages := []kafka.Message{
			{
				Topic: "events",
				Key:   "order",
				Value: "1",
				Time:  time.Now().UnixNano(),
			},
			{
				Topic: "alerts",
				Key:   "CPU",
				Value: "92",
				Time:  time.Now().UnixNano(),
			},
		}

		p := kafka.Preset(
			kafka.WithTopics("topic-1"),
			kafka.WithTopicConfigs(kafka.TopicConfig{
				Topic:         "topic-2",
				NumPartitions: 3,
			}),
			kafka.WithMessages(messages...),
			kafka.WithVersion(version),
			kafka.WithMessagesFile("./testdata/messages.json"),
		)

		container, err := gnomock.Start(
			p,
			gnomock.WithContainerName("kafka"),
			gnomock.WithTimeout(time.Minute*10),
		)
		require.NoError(t, err)

		defer func() { require.NoError(t, gnomock.Stop(container)) }()

		ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
		defer cancel()

		alertsReader := kafkaclient.NewReader(kafkaclient.ReaderConfig{
			Brokers: []string{container.Address(kafka.BrokerPort)},
			Topic:   "alerts",
		})

		m, err := alertsReader.ReadMessage(ctx)
		require.NoError(t, err)
		require.NoError(t, alertsReader.Close())

		require.Equal(t, "CPU", string(m.Key))
		require.Equal(t, "92", string(m.Value))

		eventsReader := kafkaclient.NewReader(kafkaclient.ReaderConfig{
			Brokers: []string{container.Address(kafka.BrokerPort)},
			Topic:   "events",
		})

		m, err = eventsReader.ReadMessage(ctx)
		require.NoError(t, err)
		require.NoError(t, eventsReader.Close())

		require.Equal(t, "order", string(m.Key))
		require.Equal(t, "1", string(m.Value))

		c, err := kafkaclient.Dial("tcp", container.Address(kafka.BrokerPort))
		require.NoError(t, err)

		// Test that topic-1 exists, and topic-2 has all 3 partitions
		topicReader := kafkaclient.NewReader(kafkaclient.ReaderConfig{
			Brokers: []string{container.Address(kafka.BrokerPort)},
			Topic:   "topic-1",
		})
		_, err = topicReader.ReadLag(ctx)
		require.NoError(t, err)
		require.NoError(t, topicReader.Close())

		for i := 0; i < 3; i++ {
			topicReader := kafkaclient.NewReader(kafkaclient.ReaderConfig{
				Brokers:   []string{container.Address(kafka.BrokerPort)},
				Topic:     "topic-2",
				Partition: i,
			})
			_, err = topicReader.ReadLag(ctx)
			require.NoError(t, err)
			require.NoError(t, topicReader.Close())
		}

		require.NoError(t, c.DeleteTopics("topic-1", "topic-2"))
		require.Error(t, c.DeleteTopics("unknown-topic"))

		require.NoError(t, c.Close())
	}
}

func TestPreset_withDefaults(t *testing.T) {
	p := kafka.Preset()
	container, err := gnomock.Start(
		p,
		gnomock.WithContainerName("kafka-default"),
		gnomock.WithTimeout(time.Minute*10),
	)
	require.NoError(t, err)

	defer func() { require.NoError(t, gnomock.Stop(container)) }()

	c, err := kafkaclient.Dial("tcp", container.Address(kafka.BrokerPort))
	require.NoError(t, err)
	require.NoError(t, c.Close())
}

func TestPreset_withSchemaRegistry(t *testing.T) {
	p := kafka.Preset(kafka.WithSchemaRegistry())
	container, err := gnomock.Start(
		p,
		gnomock.WithContainerName("kafka-with-registry"),
		gnomock.WithTimeout(time.Minute*10),
	)
	require.NoError(t, err)

	defer func() { require.NoError(t, gnomock.Stop(container)) }()

	c, err := kafkaclient.Dial("tcp", container.Address(kafka.BrokerPort))
	require.NoError(t, err)
	require.NoError(t, c.Close())

	out, err := http.Get("http://" + container.Address(kafka.SchemaRegistryPort))
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, out.StatusCode)
	require.NoError(t, out.Body.Close())
}
