package app

import (
	"context"
	"fmt"
	"time"

	"cwgrpguploader/internal/storage"
)

func (s *runState) handleCommand(ctx context.Context, command Command) error {
	switch command.Kind {
	case "upload":
		s.poller.requestEmission()
		// Manual retry does not bypass Retry-After or stability checks.
	case "pause":
		if err := s.toggleAutomatic(); err != nil {
			return err
		}
	case "folder":
		if err := s.changeFolder(command.Value); err != nil {
			return err
		}
	default:
		return nil
	}
	s.tick(ctx, time.Now())
	return nil
}

func (s *runState) toggleAutomatic() error {
	config := s.app.Config
	config.Automatic = !config.Automatic
	if err := s.saveConfig(config); err != nil {
		return err
	}
	s.logger.Printf("automatic uploads set to %t", config.Automatic)
	return nil
}

func (s *runState) changeFolder(path string) error {
	expanded, err := storage.ExpandPath(path)
	if err != nil {
		return err
	}
	config := s.app.Config
	config.SaveFolder = expanded
	if err := s.saveConfig(config); err != nil {
		return err
	}
	s.resetPoller(expanded)
	s.logger.Printf("save folder set to %q", expanded)
	return nil
}

func (s *runState) saveConfig(config storage.Config) error {
	if err := storage.SaveConfig(s.app.Dir, config); err != nil {
		return fmt.Errorf("Save config: %w", err)
	}
	s.app.Config = config
	return nil
}
