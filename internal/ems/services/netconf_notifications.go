package services

import (
	"context"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/fcaps/alarms"
)

type NetconfNotificationService struct {
	Bus   *bus.Bus
	Queue *alarms.NotificationQueue
	Log   zerolog.Logger
}

func NewNetconfNotificationService(b *bus.Bus, q *alarms.NotificationQueue, log zerolog.Logger) *NetconfNotificationService {
	return &NetconfNotificationService{Bus: b, Queue: q, Log: log}
}

func (s *NetconfNotificationService) Name() string { return "netconf_notifications" }

func (s *NetconfNotificationService) Run(ctx context.Context) error {
	if s.Bus == nil || s.Queue == nil {
		return nil
	}
	sub := s.Bus.Subscribe(ctx)
	for {
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-sub:
			if !ok {
				return nil
			}
			evt, ok := msg.(alarms.Event)
			if !ok {
				continue
			}
			if err := s.Queue.AppendAlarmEvent(evt); err != nil {
				s.Log.Warn().Err(err).Str("alarm_id", evt.Alarm.AlarmID).Msg("failed to queue netconf alarm notification")
			}
		}
	}
}
