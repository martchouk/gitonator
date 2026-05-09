package main

import (
	"context"
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
			n, err := s.store.RecoverStaleTasks(s.cfg.StaleAfterSeconds)
			if err != nil {
				s.logger.Println("recovery error:", err)
				continue
			}
			if n > 0 {
				s.logger.Printf("recovered %d stale task(s)", n)
			}
		}
	}
}
