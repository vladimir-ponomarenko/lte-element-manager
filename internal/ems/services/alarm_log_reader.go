package services

import (
	"bufio"
	"context"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"lte-element-manager/internal/ems/bus"
	"lte-element-manager/internal/ems/domain"
	"lte-element-manager/internal/ems/fcaps/alarms"
)

type AlarmLogReader struct {
	Path                  string
	ManagedObjectInstance string
	Bus                   *bus.Bus
	Manager               *alarms.Manager
	Log                   zerolog.Logger
}

func NewAlarmLogReader(path, moi string, b *bus.Bus, manager *alarms.Manager, log zerolog.Logger) *AlarmLogReader {
	return &AlarmLogReader{
		Path:                  strings.TrimSpace(path),
		ManagedObjectInstance: strings.TrimSpace(moi),
		Bus:                   b,
		Manager:               manager,
		Log:                   log,
	}
}

func (s *AlarmLogReader) Name() string { return "alarm_log_reader" }

func (s *AlarmLogReader) Run(ctx context.Context) error {
	if s.Path == "" || s.Manager == nil {
		return nil
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	var offset int64
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			next, err := s.readNewLines(offset)
			if err != nil {
				if !os.IsNotExist(err) {
					s.Log.Debug().Err(err).Str("path", s.Path).Msg("alarm log read failed")
				}
				continue
			}
			offset = next
		}
	}
}

func (s *AlarmLogReader) readNewLines(offset int64) (int64, error) {
	f, err := os.Open(s.Path)
	if err != nil {
		return offset, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return offset, err
	}
	if info.Size() < offset {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return offset, err
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		alarm, action, ok := mapSrsENBAlarmLine(scanner.Text(), s.ManagedObjectInstance)
		if !ok {
			continue
		}
		if action == "clear" {
			events := s.Manager.ClearComponent(time.Now().UTC(), "srsenb", "healthy")
			for _, evt := range events {
				if s.Bus != nil {
					s.Bus.Publish(evt)
				}
			}
			continue
		}
		if action == "raise" {
			evt, changed := s.Manager.Raise(time.Now().UTC(), "srsenb", "degraded", alarm)
			if changed && s.Bus != nil {
				s.Bus.Publish(evt)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return offset, err
	}
	next, err := f.Seek(0, io.SeekCurrent)
	if err != nil {
		return offset, err
	}
	return next, nil
}

func mapSrsENBAlarmLine(line, moi string) (domain.Alarm, string, bool) {
	text := strings.TrimSpace(line)
	if text == "" {
		return domain.Alarm{}, "", false
	}
	lower := strings.ToLower(text)
	if strings.Contains(lower, "clear") || strings.Contains(lower, "restored") || strings.Contains(lower, "recovered") {
		return domain.Alarm{}, "clear", true
	}
	switch {
	case strings.Contains(lower, "s1") || strings.Contains(lower, "mme"):
		return domain.Alarm{
			Code:                  alarms.AlarmS1Down,
			Message:               text,
			ManagedObjectInstance: moi,
		}, "raise", true
	case strings.Contains(lower, "error") || strings.Contains(lower, "failed") || strings.Contains(lower, "alarm"):
		return domain.Alarm{
			Code:                  alarms.AlarmGenericEMS,
			Message:               text,
			ManagedObjectInstance: moi,
		}, "raise", true
	default:
		return domain.Alarm{}, "", false
	}
}
