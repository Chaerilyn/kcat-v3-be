package main

import (
	"log"

	"github.com/pocketbase/pocketbase"
	"github.com/pocketbase/pocketbase/core"
	// "github.com/bwmarrin/discordgo" // Uncomment when you add your bot
)

func main() {
	app := pocketbase.New()

	// ---------------------------------------------------------------
	// FIX 1: Use BindFunc (instead of Add)
	// ---------------------------------------------------------------
	app.OnServe().BindFunc(func(e *core.ServeEvent) error {

		// Start your Discord bot in a separate goroutine
		go func() {
			log.Println("🤖 Starting Discord Bot...")

			// ---------------------------------------------------------------
			// FIX 2: Query the DB directly from 'app' (No more app.Dao())
			// ---------------------------------------------------------------
			// Example: Find a user by ID
			// record, err := app.FindRecordById("users", "RECORD_ID")
			// if err != nil {
			//     log.Println("Error finding user:", err)
			// } else {
			//     log.Println("Found user:", record.GetString("email"))
			// }

			// Start your bot here...
			// dg.Open()
		}()

		// ---------------------------------------------------------------
		// FIX 3: You must call e.Next() to continue server startup
		// ---------------------------------------------------------------
		return e.Next()
	})

	if err := app.Start(); err != nil {
		log.Fatal(err)
	}
}
