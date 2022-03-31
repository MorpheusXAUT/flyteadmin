package implementations

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"github.com/flyteorg/flyteidl/gen/pb-go/flyteidl/admin"

	"github.com/flyteorg/flyteadmin/pkg/async/notifications/implementations"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/flyteorg/flyteadmin/pkg/async/notifications/interfaces"

	"github.com/flyteorg/flytestdlib/logger"
	"github.com/flyteorg/flytestdlib/promutils"
	"github.com/golang/protobuf/proto"
)

const (
	cloudEventSource     = "https://github.com/flyteorg/flyteadmin"
	cloudEventTypePrefix = "com.flyte.resource"
	jsonSchemaURLKey     = "jsonschemaurl"
	jsonSchemaURL        = "https://github.com/flyteorg/flyteidl/blob/cloudevent2/jsonschema/workflow_execution.json"
)

// Publisher This event publisher acts to asynchronously publish workflow execution events.
type Publisher struct {
	sender        Sender
	systemMetrics implementations.EventPublisherSystemMetrics
	events        sets.String
}

func isInstanceOf(objectPtr, typePtr interface{}) bool {
	return reflect.TypeOf(objectPtr) == reflect.TypeOf(typePtr)
}

func (p *Publisher) Publish(ctx context.Context, notificationType string, msg proto.Message) error {
	if !p.shouldPublishEvent(notificationType) {
		return nil
	}
	p.systemMetrics.PublishTotal.Inc()
	logger.Debugf(ctx, "Publishing the following message [%+v]", msg)

	var executionID string
	var phase string
	var eventTime time.Time

	switch reflect.TypeOf(msg) {
	case reflect.TypeOf(&admin.WorkflowExecutionEventRequest{}):
		e := msg.(*admin.WorkflowExecutionEventRequest).Event
		executionID = e.ExecutionId.String()
		phase = e.Phase.String()
		eventTime = e.OccurredAt.AsTime()
	case reflect.TypeOf(&admin.TaskExecutionEventRequest{}):
		e := msg.(*admin.TaskExecutionEventRequest).Event
		executionID = e.TaskId.String()
		phase = e.Phase.String()
		eventTime = e.OccurredAt.AsTime()
	case reflect.TypeOf(&admin.NodeExecutionEventRequest{}):
		e := msg.(*admin.NodeExecutionEventRequest).Event
		executionID = msg.(*admin.NodeExecutionEventRequest).Event.Id.String()
		phase = e.Phase.String()
		eventTime = e.OccurredAt.AsTime()
	default:
		return fmt.Errorf("unsupported event types [%+v]", reflect.TypeOf(msg))
	}

	event := cloudevents.NewEvent()
	// CloudEvent specification: https://github.com/cloudevents/spec/blob/v1.0/spec.md#required-attributes
	event.SetType(fmt.Sprintf("%v.%v", cloudEventTypePrefix, notificationType))
	event.SetSource(cloudEventSource)
	event.SetID(fmt.Sprintf("%v.%v", executionID, phase))
	event.SetTime(eventTime)
	event.SetExtension(jsonSchemaURLKey, jsonSchemaURL)

	if err := event.SetData(cloudevents.ApplicationJSON, &msg); err != nil {
		p.systemMetrics.PublishError.Inc()
		logger.Errorf(ctx, "Failed to encode message [%v] with error: %v", msg, err)
		return err
	}

	if err := p.sender.Send(ctx, notificationType, event); err != nil {
		p.systemMetrics.PublishError.Inc()
		logger.Errorf(ctx, "Failed to send message [%v] with error: %v", msg, err)
		return err
	}
	p.systemMetrics.PublishSuccess.Inc()
	return nil
}

func (p *Publisher) shouldPublishEvent(notificationType string) bool {
	return p.events.Has(notificationType)
}

func NewCloudEventsPublisher(sender Sender, scope promutils.Scope, eventTypes []string) interfaces.Publisher {
	eventSet := sets.NewString()

	for _, event := range eventTypes {
		if event == implementations.AllTypes || event == implementations.AllTypesShort {
			for _, e := range implementations.SupportedEvents {
				eventSet = eventSet.Insert(e)
			}
			break
		}
		if e, found := implementations.SupportedEvents[event]; found {
			eventSet = eventSet.Insert(e)
		} else {
			panic(fmt.Errorf("unsupported event type [%s] in the config", event))
		}
	}

	return &Publisher{
		sender:        sender,
		systemMetrics: implementations.NewEventPublisherSystemMetrics(scope.NewSubScope("cloudevents_publisher")),
		events:        eventSet,
	}
}
