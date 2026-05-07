package main

import (
	"log"
	"os"

	_ "github.com/joho/godotenv/autoload"

	"github.com/PurgeBot-net/interactions/internal/commands"
	"github.com/disgoorg/disgo"
	"github.com/disgoorg/snowflake/v2"
)

func main() {
	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		log.Fatal("DISCORD_TOKEN is required")
	}
	appIDStr := os.Getenv("DISCORD_APPLICATION_ID")
	if appIDStr == "" {
		log.Fatal("DISCORD_APPLICATION_ID is required")
	}
	appID, err := snowflake.Parse(appIDStr)
	if err != nil {
		log.Fatalf("invalid DISCORD_APPLICATION_ID: %v", err)
	}

	client, err := disgo.New(token)
	if err != nil {
		log.Fatalf("failed to create client: %v", err)
	}

	if _, err := client.Rest.SetGlobalCommands(appID, commands.GlobalCommands()); err != nil {
		log.Fatalf("failed to register commands: %v", err)
	}

	log.Println("Commands registered successfully!")
}
