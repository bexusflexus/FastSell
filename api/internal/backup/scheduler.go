package backup

import (
	"errors"
	"fmt"
	"sync"

	"github.com/robfig/cron/v3"
)

var cronParser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

func ValidateCron(expression, timezone string) error {
	if _, err := cronParser.Parse(expression); err != nil {
		return errors.New("cron_expression must be a valid five-field cron expression")
	}
	if timezone == "" {
		timezone = "UTC"
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	if _, err := parser.Parse(fmt.Sprintf("CRON_TZ=%s %s", timezone, expression)); err != nil {
		return errors.New("cron_expression or timezone is invalid")
	}
	return nil
}

type Scheduler struct {
	mu      sync.Mutex
	cron    *cron.Cron
	entryID cron.EntryID
	run     func()
}

func NewScheduler(run func()) *Scheduler {
	return &Scheduler{cron: cron.New(), run: run}
}

func (s *Scheduler) Start(settings Settings) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entryID != 0 {
		s.cron.Remove(s.entryID)
		s.entryID = 0
	}
	if !settings.AutomaticEnabled {
		return nil
	}
	if err := ValidateCron(settings.CronExpression, settings.Timezone); err != nil {
		return err
	}
	id, err := s.cron.AddFunc(fmt.Sprintf("CRON_TZ=%s %s", settings.Timezone, settings.CronExpression), s.run)
	if err != nil {
		return err
	}
	s.entryID = id
	s.cron.Start()
	return nil
}

func (s *Scheduler) Stop() {
	s.mu.Lock()
	ctx := s.cron.Stop()
	s.mu.Unlock()
	<-ctx.Done()
}
