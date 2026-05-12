package main

import (
	"context"
	"strings"
	"time"
)

func (s *Server) runRecoveryLoop(ctx context.Context) {
	interval := time.Duration(s.cfg.RecoverEverySeconds) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	s.logger.Printf("recovery loop started: every=%s stale_after=%ds", interval.String(), s.cfg.StaleAfterSeconds)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			bridges, err := s.store.RecoverStaleTasksWithLog(s.cfg.StaleAfterSeconds)
			if err != nil {
				s.logger.Println("recovery error:", err)
				continue
			}
			if len(bridges) > 0 {
				s.logger.Printf("recovered %d stale dispatched task(s) from bridge(s): %s",
					len(bridges), strings.Join(bridges, ", "))
			} else {
				s.debugf("recovery tick: no stale tasks")
			}
		}
	}
}
