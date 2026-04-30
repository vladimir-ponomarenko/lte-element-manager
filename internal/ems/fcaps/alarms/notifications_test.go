package alarms

import (
	"strings"
	"testing"
	"time"

	"lte-element-manager/internal/ems/domain"
)

func TestNotificationQueueAppendAndDrainAlarmEvent(t *testing.T) {
	q := NewNotificationQueue(2)
	err := q.AppendAlarmEvent(Event{
		At:        time.Date(2026, 5, 6, 1, 2, 3, 0, time.UTC),
		Component: "srsenb",
		Status:    StatusActive,
		Count:     3,
		Alarm: domain.Alarm{
			AlarmID:               AlarmS1Down,
			ManagedObjectInstance: "SubNetwork=srsRAN/ManagedElement=enb1/ENBFunction=1",
			EventType:             EventTypeCommunicationsAlarm,
			ProbableCause:         ProbableCauseCommunicationsFail,
			PerceivedSeverity:     SeverityCritical,
			SpecificProblem:       "S1AP connectivity is down",
		},
	})
	if err != nil {
		t.Fatalf("append failed: %v", err)
	}
	items := q.Drain(10)
	if len(items) != 1 {
		t.Fatalf("expected 1 notification, got %d", len(items))
	}
	if !strings.Contains(items[0].Payload, "ems-fault-management:alarm-notification") {
		t.Fatalf("unexpected payload: %s", items[0].Payload)
	}
	if q.Len() != 0 {
		t.Fatalf("expected drained queue")
	}
}

func TestNotificationQueueCapacityDropsOldest(t *testing.T) {
	q := NewNotificationQueue(1)
	q.Append(Notification{Payload: `{"x":1}`})
	q.Append(Notification{Payload: `{"x":2}`})
	items := q.Drain(10)
	if len(items) != 1 || !strings.Contains(items[0].Payload, "2") {
		t.Fatalf("expected newest notification only: %+v", items)
	}
}
