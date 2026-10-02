package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/devpopsdotin/k9s-deck/internal/k8s"
	"github.com/devpopsdotin/k9s-deck/internal/logger"
	"github.com/devpopsdotin/k9s-deck/internal/ui"
)

func main() {
	var cfg ui.Config
	if len(os.Args) < 4 {
		if os.Getenv("KUBECONFIG") != "" {
			cfg.Context = "kind-kind"
			cfg.Namespace = "default"
			cfg.Deployment = "hello-app"
		} else {
			fmt.Println("Usage: k9s-deck <context> <namespace> <deployment>")
			os.Exit(1)
		}
	} else {
		cfg.Context = os.Args[1]
		cfg.Namespace = os.Args[2]
		cfg.Deployment = os.Args[3]
	}

	// Initialize logger (writes to logger.LogPath(), /tmp/k9s-deck.log on Unix)
	if err := logger.Init(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to initialize logger: %v\n", err)
		// Continue anyway - logging is not critical
	}

	// Initialize Kubernetes client (uses client-go for performance)
	client, err := k8s.NewClient(cfg.Context)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to create Kubernetes client: %v\n", err)
		os.Exit(1)
	}

	cfg.Client = client

	p := tea.NewProgram(ui.NewModel(cfg), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}
