package main

import (
	"flag"
	"fmt"
	"go-upkeep/internal/db"
	"go-upkeep/internal/monitor"
	"go-upkeep/internal/tui"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/ssh"
	"github.com/charmbracelet/wish"
	bm "github.com/charmbracelet/wish/bubbletea"
	"github.com/mattn/go-isatty"
)

func main() {
	log.SetOutput(io.Discard)

	bindPort := flag.Int("port", 23234, "SSH Port to listen on")
	dbPath := flag.String("db", "upkeep.db", "Path to SQLite database")
	keysPath := flag.String("keys", "authorized_keys", "Path to authorized_keys file")
	flag.Parse()

	db.Init(*dbPath)
	monitor.StartEngine()
	
	startSSHServer(*bindPort, *keysPath)

	if isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd()) {
		p := tea.NewProgram(tui.InitialModel(), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Printf("Error: %v\n", err)
		}
	} else {
		fmt.Println("Go-Upkeep running in HEADLESS mode (Background Service)")
		
		done := make(chan os.Signal, 1)
		signal.Notify(done, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
		<-done
		fmt.Println("Shutting down...")
	}
}

func startSSHServer(port int, authKeysPath string) {
	s, err := wish.NewServer(
		wish.WithAddress(fmt.Sprintf(":%d", port)),
		wish.WithHostKeyPath(".ssh/id_ed25519"),
		
		wish.WithPublicKeyAuth(func(ctx ssh.Context, key ssh.PublicKey) bool {
			data, err := os.ReadFile(authKeysPath)
			if err != nil {
				return false 
			}

			return isKeyAllowed(data, key)
		}),

		wish.WithMiddleware(
			bm.Middleware(func(s ssh.Session) (tea.Model, []tea.ProgramOption) {
				return tui.InitialModel(), []tea.ProgramOption{tea.WithAltScreen()}
			}),
		),
	)
	if err != nil {
		return
	}

	go func() {
		s.ListenAndServe()
	}()
}

func isKeyAllowed(authFileData []byte, incomingKey ssh.PublicKey) bool {
	for len(authFileData) > 0 {
		allowedKey, _, _, rest, err := ssh.ParseAuthorizedKey(authFileData)
		if err != nil {
			authFileData = rest
			continue
		}
		
		if ssh.KeysEqual(allowedKey, incomingKey) {
			return true
		}
		
		authFileData = rest
	}
	return false
}