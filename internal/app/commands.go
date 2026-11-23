package app

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"cwgrpguploader/internal/storage"
)

func (s *runState) handleCommand(ctx context.Context, command Command) error {
	switch command.Kind {
	case "upload":
		s.poller.requestEmission()
		// Manual retry does not bypass Retry-After or stability checks.
	case "retry-automatic":
		// Authentication can finish before the UI receives a settings update.
		// Only the coordinator's current preference may create automatic work.
		if !s.app.Config.Automatic {
			return nil
		}
		s.poller.requestEmission()
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
	config := s.app.Config
	config.SaveFolder = filepath.Clean(path)
	if err := s.saveConfig(config); err != nil {
		return err
	}
	s.resetPoller(config.SaveFolder)
	s.logger.Printf("save folder set to %q", config.SaveFolder)
	return nil
}

func (s *runState) saveConfig(config storage.Config) error {
	dir := s.app.ConfigDir
	if dir == "" {
		dir = s.app.Dir
	}
	if err := storage.SaveConfig(dir, config); err != nil {
		return fmt.Errorf("Save config: %w", err)
	}
	s.app.Config = config
	return nil
}
