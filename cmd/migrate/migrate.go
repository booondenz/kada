// Command migrate applies the database schema without starting the API server.
//
// The schema is defined by the GORM models in internal/domain/entity; this binary just runs
// AutoMigrate against DATABASE_URL. It is the recommended way to evolve the schema in production
// (run it before restarting the API), while the API server itself only migrates when
// DB_AUTO_MIGRATE=true, so a deployment can never change the schema as a side effect of a restart.
package main

import (
	"log"

	"github.com/joho/godotenv"

	"github.com/chun/kada-backend/config"
	"github.com/chun/kada-backend/internal/infra"
)

func main() {
	_ = godotenv.Load()

	cfg := config.Load()

	log.Println("Applying database schema (GORM AutoMigrate)...")
	db, err := infra.NewDB(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	defer infra.CloseDB(db)

	if err := infra.Migrate(db); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}
	log.Println("Migration done.")
}
