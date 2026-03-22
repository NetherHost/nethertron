package main

import (
	"bufio"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/bwmarrin/discordgo"
	_ "github.com/mattn/go-sqlite3"
)

var (
	discordSession *discordgo.Session
	db             *sql.DB
	dbMutex        sync.RWMutex
)

func main() {
	fmt.Println("Program started!")
	fmt.Printf("%sStarting bot...%s\n", ColorCyan, ColorReset)

	if err := loadSecretsAndURLs(); err != nil {
		fmt.Printf("%sERROR: %v%s\n", ColorRed, err, ColorReset)
		log.Fatalf("%sConfiguration error: %v%s", ColorRed, err, ColorReset)
	}

	// Initialize database
	fmt.Printf("%sInitializing database...%s\n", ColorCyan, ColorReset)
	var err error
	db, err = initDatabase()
	if err != nil {
		fmt.Printf("%sERROR: Database init failed: %v%s\n", ColorRed, err, ColorReset)
		log.Fatalf("%sError initializing database: %v%s", ColorRed, err, ColorReset)
	}
	fmt.Printf("%s✓ Database initialized%s\n", ColorGreen, ColorReset)

	// Create a new Discord session
	fmt.Printf("%sCreating Discord session...%s\n", ColorCyan, ColorReset)
	dg, err := discordgo.New("Bot " + BotToken)
	if err != nil {
		log.Fatal("Error creating Discord session:", err)
	}

	// Store session globally for status updates
	discordSession = dg

	// Register interaction handler for slash commands
	dg.AddHandler(interactionCreate)

	// Register ready handler
	dg.AddHandler(ready)

	// Register guild member add handler for auto-role
	dg.AddHandler(guildMemberAdd)

	// Enable intents (including GuildMembers for member join events)
	dg.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsGuilds | discordgo.IntentsGuildMembers

	// Open a websocket connection to Discord
	fmt.Printf("%sConnecting to Discord...%s\n", ColorCyan, ColorReset)
	err = dg.Open()
	if err != nil {
		log.Fatal("Error opening connection:", err)
	}
	fmt.Printf("%s✓ Connected to Discord%s\n", ColorGreen, ColorReset)

	// Register slash commands
	fmt.Printf("%sRegistering commands...%s\n", ColorCyan, ColorReset)
	registerCommands(dg)
	fmt.Printf("%s✓ Commands registered%s\n", ColorGreen, ColorReset)

	// Start periodic status updates
	go updateStatusPeriodically()

	// Start API server to receive updates from Laravel
	startAPIServer()

	// Create a channel to signal shutdown
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	// Start a goroutine to read from stdin
	go func() {
		scanner := bufio.NewScanner(os.Stdin)
		fmt.Printf("%sBot is now running. Type 'q' and press Enter to stop, or press CTRL-C to exit.%s\n", ColorGreen, ColorReset)
		for scanner.Scan() {
			input := strings.TrimSpace(scanner.Text())
			if strings.ToLower(input) == "q" {
				fmt.Printf("%sShutting down bot...%s\n", ColorYellow, ColorReset)
				stop <- os.Interrupt
				return
			}
		}
	}()

	// Wait for stop signal
	<-stop

	// Cleanly close down the Discord session
	fmt.Printf("%sClosing Discord session...%s\n", ColorYellow, ColorReset)
	dg.Close()

	// Close database connection
	if db != nil {
		fmt.Printf("%sClosing database connection...%s\n", ColorYellow, ColorReset)
		db.Close()
	}

	fmt.Printf("%sBot stopped.%s\n", ColorRed, ColorReset)
}

// This function will be called when the bot receives the "ready" event from Discord
func ready(s *discordgo.Session, event *discordgo.Ready) {
	fmt.Printf("%s✓ Logged in as: %s%v#%v%s\n", ColorGreen, ColorCyan, event.User.Username, event.User.Discriminator, ColorReset)
}
