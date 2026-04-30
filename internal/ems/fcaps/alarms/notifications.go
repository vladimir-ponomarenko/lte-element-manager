package alarms

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"
)

const DefaultNotificationQueueCapacity = 1024

type Notification struct {
	ID        uint64
	EventTime time.Time
	Payload   string
}

type NotificationQueue struct {
	mu       sync.Mutex
	nextID   uint64
	cap      int
	messages []Notification
}

func NewNotificationQueue(capacity int) *NotificationQueue {
	if capacity <= 0 {
		capacity = DefaultNotificationQueueCapacity
	}
	return &NotificationQueue{cap: capacity}
}

func (q *NotificationQueue) AppendAlarmEvent(evt Event) error {
	if q == nil {
		return nil
	}
	at := evt.At.UTC()
	if at.IsZero() {
		at = time.Now().UTC()
	}
	status := string(evt.Status)
	if status == "" {
		status = string(StatusActive)
	}
	payload := map[string]any{
		"ems-fault-management:alarm-notification": map[string]any{
			"alarm_id":                evt.Alarm.AlarmID,
			"managed_object_instance": evt.Alarm.ManagedObjectInstance,
			"event_type":              evt.Alarm.EventType,
			"probable_cause":          evt.Alarm.ProbableCause,
			"perceived_severity":      evt.Alarm.PerceivedSeverity,
			"specific_problem":        evt.Alarm.SpecificProblem,
			"event_time":              at.Format(time.RFC3339),
			"occurrence_count":        strconv.FormatUint(evt.Count, 10),
			"alarm_status":            status,
			"component":               evt.Component,
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	q.Append(Notification{EventTime: at, Payload: string(b)})
	return nil
}

func (q *NotificationQueue) Append(msg Notification) {
	if q == nil || msg.Payload == "" {
		return
	}
	if msg.EventTime.IsZero() {
		msg.EventTime = time.Now().UTC()
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	q.nextID++
	msg.ID = q.nextID
	q.messages = append(q.messages, msg)
	if len(q.messages) > q.cap {
		copy(q.messages, q.messages[len(q.messages)-q.cap:])
		q.messages = q.messages[:q.cap]
	}
}

func (q *NotificationQueue) Drain(max int) []Notification {
	if q == nil {
		return nil
	}
	if max <= 0 {
		max = 100
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.messages) == 0 {
		return nil
	}
	if max > len(q.messages) {
		max = len(q.messages)
	}
	out := append([]Notification(nil), q.messages[:max]...)
	copy(q.messages, q.messages[max:])
	q.messages = q.messages[:len(q.messages)-max]
	return out
}

func (q *NotificationQueue) Len() int {
	if q == nil {
		return 0
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.messages)
}
