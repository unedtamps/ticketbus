package kafka

import (
	"context"

	kafkago "github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// headerCarrier adapts Kafka message headers to the OpenTelemetry
// propagation.TextMapCarrier interface. kafka-go ships no propagator adapter,
// and the otelhttp equivalent does not apply to message headers.
type headerCarrier struct {
	headers *[]kafkago.Header
}

func (c headerCarrier) Get(key string) string {
	for _, h := range *c.headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c headerCarrier) Set(key, value string) {
	for i := range *c.headers {
		if (*c.headers)[i].Key == key {
			(*c.headers)[i].Value = []byte(value)
			return
		}
	}
	*c.headers = append(*c.headers, kafkago.Header{Key: key, Value: []byte(value)})
}

func (c headerCarrier) Keys() []string {
	keys := make([]string, 0, len(*c.headers))
	for _, h := range *c.headers {
		keys = append(keys, h.Key)
	}
	return keys
}

// injectContext writes the current span context into the message headers so the
// consumer can continue the trace. A context with no recording span produces no
// headers at all, so uninstrumented producers keep sending plain messages and
// old messages stay readable by new consumers.
func injectContext(ctx context.Context, headers *[]kafkago.Header) {
	otel.GetTextMapPropagator().Inject(ctx, headerCarrier{headers: headers})
}

// startConsumerSpan extracts any propagated context and starts a CONSUMER span
// parented to it, so a message handled in another service lands in the same
// trace as the request that produced it.
//
// Kafka is at-least-once, so a redelivered message can appear twice in one
// trace as two consumer spans sharing the same messaging.message.id. That pair
// is what reveals a redelivery.
func startConsumerSpan(ctx context.Context, msg Message) (context.Context, trace.Span) {
	carrier := headerCarrier{headers: &msg.Headers}
	parent := otel.GetTextMapPropagator().Extract(ctx, carrier)

	attrs := []attribute.KeyValue{
		semconv.MessagingSystemKafka,
		semconv.MessagingDestinationName(msg.Topic),
		semconv.MessagingOperationTypeReceive,
		semconv.MessagingKafkaMessageKey(msg.Key),
		semconv.MessagingKafkaOffset(int(msg.Offset)),
	}

	return otel.Tracer("github.com/nedo/TicketSaas/pkg/kafka").
		Start(parent, msg.Topic+" process",
			trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithAttributes(attrs...))
}

// recordConsumerError marks a span as failed. Kept separate so handlers can
// report failures against the consumer span without holding a reference to it.
func recordConsumerError(span trace.Span, err error) {
	if err == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}

var _ propagation.TextMapCarrier = headerCarrier{}
