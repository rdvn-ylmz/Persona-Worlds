package main

import (
	"context"
	"log"

	"personaworlds/backend/internal/config"
	"personaworlds/backend/internal/db"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("db connect failed: %v", err)
	}
	defer pool.Close()

	if err := db.RunMigrations(ctx, pool, cfg.MigrationsDir); err != nil {
		log.Fatalf("migration failed: %v", err)
	}

	rooms := []struct {
		Slug        string
		Name        string
		Description string
	}{
		{Slug: "go-backend", Name: "go-backend", Description: "Backend architecture, APIs, and Go production patterns."},
		{Slug: "cybersecurity", Name: "cybersecurity", Description: "Security ops, threat modeling, and secure engineering."},
		{Slug: "game-dev", Name: "game-dev", Description: "Gameplay systems, engines, monetization, and design."},
		{Slug: "ai-builders", Name: "ai-builders", Description: "Shipping AI products, evals, prompts, and ops."},
	}

	for _, room := range rooms {
		if _, err := pool.Exec(ctx, `
			INSERT INTO rooms(slug, name, description)
			VALUES ($1, $2, $3)
			ON CONFLICT (slug) DO UPDATE
			SET name=EXCLUDED.name, description=EXCLUDED.description
		`, room.Slug, room.Name, room.Description); err != nil {
			log.Fatalf("seed room %s failed: %v", room.Slug, err)
		}
	}

	log.Printf("seed completed: %d rooms", len(rooms))
}
