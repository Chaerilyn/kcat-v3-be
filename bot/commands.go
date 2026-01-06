package bot

import (
	"encoding/json"
	"fmt"
	"io"
	"kcat-v3-be/bot/utils"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

var registeredCommands = make([]*discordgo.ApplicationCommand, 0)

type PaginationState struct {
	Pages     []string
	Page      int
	CreatedAt time.Time
}

// paginationStates stores user-specific pagination state using keys of format "userID:messageID"
// This ensures each user has their own pagination state and cannot interfere with others
var paginationStates = make(map[string]PaginationState)

// getUserID safely extracts the user ID from an interaction
// Handles both guild interactions (via Member) and DM interactions (via User)
func getUserID(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}

// getPaginationKey creates a user-specific pagination key
// Format: "userID:messageID" to ensure each user has isolated pagination state
func getPaginationKey(userID, messageID string) string {
	return fmt.Sprintf("%s:%s", userID, messageID)
}

func registerSlashCommands(s *discordgo.Session) {
	commands := []*discordgo.ApplicationCommand{
		{
			Name:        "revive",
			Description: "Retrieve file based on a imgur link.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "imgur_link",
					Description: "The imgur link (e.g. 'https://i.imgur.com/abc123.mp4')",
					Type:        discordgo.ApplicationCommandOptionString,
					Required:    true,
				},
			},
		},
		{
			Name:        "unwrap",
			Description: "Unwrap a KpopCat set link with interactive pagination",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "kcat_set_link",
					Description: "A link like 'https://kpopcat.pics/set/yv5dzbdxz04lap5'",
					Type:        discordgo.ApplicationCommandOptionString,
					Required:    true,
				},
				{
					Name:        "raw",
					Description: "Get raw files instead of imgur links.",
					Type:        discordgo.ApplicationCommandOptionBoolean,
					Required:    false,
				},
				{
					Name:        "perpage",
					Description: "How many links to show per page (1‑5, default 1)",
					Type:        discordgo.ApplicationCommandOptionInteger,
					Required:    false,
				},
				{
					Name:        "hide_metadata",
					Description: "Hide metadata (idol, group, etc) from the first page response.",
					Type:        discordgo.ApplicationCommandOptionBoolean,
					Required:    false,
				},
			},
		},
		{
			Name:        "source",
			Description: "Get the video source (youtube link) from imgur link.",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Name:        "imgur_link",
					Description: "The link to the gif (e.g. 'https://i.imgur.com/abc123.mp4')",
					Type:        discordgo.ApplicationCommandOptionString,
					Required:    true,
				},
			},
		},
	}

	// --- GUILD REGISTRATION ---
	// List every guild that should get the commands.
	// Add or remove IDs as you like.
	guildIDs := []string{
		"1169291742504300595",
		"1298381481739161690",
	}

	for _, gid := range guildIDs {
		for _, cmd := range commands {
			createdCmd, err := s.ApplicationCommandCreate(
				s.State.User.ID,
				gid, // register in this guild
				cmd,
			)
			if err != nil {
				log.Fatalf("Cannot create slash command %q in guild %s: %v",
					cmd.Name, gid, err)
			}
			registeredCommands = append(registeredCommands, createdCmd)
		}
	}
}

// commandUsed listens for slash commands (and other interactions).
func commandUsed(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		switch i.ApplicationCommandData().Name {
		case "revive":
			handleReviveCommand(s, i)
		case "unwrap":
			handleUnwrapCommand(s, i)
		case "source":
			handleSourceCommand(s, i)
		}
	case discordgo.InteractionMessageComponent:
		switch i.MessageComponentData().CustomID {
		case "first", "prev", "next", "last":
			handlePaginationInteraction(s, i)
		}
	}
}

func handleReviveCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Extract the mirror link from the slash command option
	mirrorLink := i.ApplicationCommandData().Options[0].StringValue()

	// Convert links from imgur.com to i.imgur.com + .mp4
	if strings.HasPrefix(mirrorLink, "https://imgur.com/") {
		mirrorLink = strings.Replace(mirrorLink, "https://imgur.com/", "https://i.imgur.com/", 1)
		mirrorLink += ".mp4"
	}

	// Use internal Pocketbase API instead of HTTP
	filter := fmt.Sprintf("mirror='%s'", strings.ReplaceAll(mirrorLink, "'", "\\'"))
	records, err := App.FindRecordsByFilter("v1", filter, "", 1, 0)

	if err != nil {
		respondWithError(s, i.Interaction, "Could not query database.")
		return
	}

	// Check if we got a record
	if len(records) == 0 {
		// Send an ephemeral message only visible to the user who invoked the command
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "No matching record found for that mirror link.",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	record := records[0]
	recordID := record.Id
	fileValue := record.GetString("file")
	kpfhdFileValue := record.GetString("kpfhdFile")

	// Decide which URL or link to respond with
	var content string
	if fileValue != "" {
		// Build your final URL: https://kcat.pics/v1/{id}/{file}
		originalURL := fmt.Sprintf("https://kcat.pics/v1/%s/%s", recordID, fileValue)
		content = fmt.Sprintf("Found copy in KpopCat: %s", originalURL)
	} else if kpfhdFileValue != "" {
		// If 'file' is empty but 'kpfhdFile' is present, just respond with that link
		content = fmt.Sprintf("Found copy in KpopCat: %s", kpfhdFileValue)
	} else {
		// Both are empty?
		content = "No 'file' or 'kpfhdFile' was available for that record."
	}

	// Respond with the final link
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
		},
	})
	if err != nil {
		log.Printf("Error responding to /revive: %v", err)
	}
}

func handleUnwrapCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	var (
		raw          bool
		perPage      int64 = 1 // default
		showMetadata bool      // default hidden unless explicitly requested
	)
	var setLink string
	// parse slash‑options regardless of order
	for _, opt := range i.ApplicationCommandData().Options {
		switch opt.Name {
		case "kcat_set_link":
			setLink = opt.StringValue()
		case "raw":
			raw = opt.BoolValue()
		case "perpage":
			perPage = opt.IntValue()
		case "hide_metadata":
			// Although the option is named "hide_metadata", per requirement we use it to SHOW metadata
			// on the first page when provided/true. By default metadata is hidden.
			if opt.BoolValue() {
				showMetadata = true
			}
		}
	}

	// 1. Determine whether the link is to a /set/ or /collection/ path
	parts := strings.Split(strings.TrimSuffix(setLink, "/"), "/")
	if len(parts) == 0 {
		respondWithError(s, i.Interaction, "Invalid link.")
		return
	}
	linkID := parts[len(parts)-1] // last path segment

	var filterStr string
	switch {
	case strings.Contains(setLink, "/set/"):
		// strict match on the single‑value "set" field
		filterStr = fmt.Sprintf("(set=\"%s\")", linkID)
	case strings.Contains(setLink, "/collection/"):
		// "collections" is an array → use ~ to match if ID is present
		filterStr = fmt.Sprintf("(collections~\"%s\")", linkID)
	default:
		respondWithError(s, i.Interaction,
			"Link must contain either /set/ or /collection/ in the path.")
		return
	}

	// 2. Build the request URL (using HTTP for now, but could be refactored to use internal API)
	baseURL := "https://kcat.pockethost.io/api/collections/contents/records"
	q := url.Values{}
	q.Set("page", "1")
	q.Set("perPage", "12")    // We'll still fetch up to 12 items
	q.Set("sort", "-created") // sorted by newest first
	q.Set("filter", filterStr)
	// expand fields to get group, idol, uploader, etc.
	q.Set("expand", "idol,group,tag,uploader,likes")

	fullURL := fmt.Sprintf("%s?%s", baseURL, q.Encode())

	resp, err := http.Get(fullURL)
	if err != nil {
		respondWithError(s, i.Interaction, "Could not contact PocketBase.")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respondWithError(s, i.Interaction,
			fmt.Sprintf("Received non-OK status from PocketBase: %d", resp.StatusCode))
		return
	}

	// 3. Parse JSON response
	// We'll define a struct that includes extra fields we want to display.
	var pbResp struct {
		Items []struct {
			ID        string `json:"id"`
			File      string `json:"file"`
			KpfhdFile string `json:"kpfhdFile"`
			Mirror    string `json:"mirror"`
			Title     string `json:"title"`
			Created   string `json:"created"`
			Expand    struct {
				Group []struct {
					Name string `json:"name"`
				} `json:"group"`
				Idol []struct {
					Name string `json:"name"`
				} `json:"idol"`
				Uploader []struct {
					Name string `json:"name"`
				} `json:"uploader"`
			} `json:"expand"`
		} `json:"items"`
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		respondWithError(s, i.Interaction, "Failed to read PocketBase response.")
		return
	}
	err = json.Unmarshal(bodyBytes, &pbResp)
	if err != nil {
		respondWithError(s, i.Interaction, "Failed to parse JSON from PocketBase.")
		return
	}

	if len(pbResp.Items) == 0 {
		respondWithError(s, i.Interaction, "No items found for that set.")
		return
	}

	// --- metadata header (same as /unwrap) ---
	firstItem := pbResp.Items[0]

	// Collect group, idol, and uploader names
	var groupNames, idolNames, uploaderNames []string
	for _, g := range firstItem.Expand.Group {
		groupNames = append(groupNames, g.Name)
	}
	for _, d := range firstItem.Expand.Idol {
		idolNames = append(idolNames, d.Name)
	}
	for _, u := range firstItem.Expand.Uploader {
		uploaderNames = append(uploaderNames, u.Name)
	}

	groupLine := strings.Join(groupNames, ", ")
	idolLine := strings.Join(idolNames, ", ")
	uploaderLine := strings.Join(uploaderNames, ", ")

	metaHeader := fmt.Sprintf(
		"**Title**: %s\n**Created**: %s\n**Groups**: %s\n**Idols**: %s\n**Uploader**: %s\n\n**Links**:",
		firstItem.Title,
		firstItem.Created,
		groupLine,
		idolLine,
		uploaderLine,
	)

	var links []string
	for _, item := range pbResp.Items {
		link := item.Mirror
		if raw || item.Mirror == "" {
			link = item.KpfhdFile
			if link == "" {
				link = utils.GenerateLinkFromFilename(item.ID, item.File)
			}
		}
		if link != "" {
			links = append(links, link)
		}
	}

	fmt.Printf("Found %d links\n", len(links))

	if len(links) == 0 {
		respondWithError(s, i.Interaction, "No usable links found.")
		return
	}

	// clamp perPage between 1 and 5
	if perPage < 1 {
		perPage = 1
	}
	if perPage > 5 {
		perPage = 5
	}

	var pages []string
	for idx := 0; idx < len(links); idx += int(perPage) {
		end := idx + int(perPage)
		if end > len(links) {
			end = len(links)
		}
		chunk := strings.Join(links[idx:end], "\n")
		if idx == 0 && showMetadata {
			chunk = metaHeader + "\n" + chunk
		}
		pages = append(pages, chunk)
	}

	// Save to some temporary store if needed (e.g., in-memory map[string][]string keyed by Interaction ID)
	// For now just send first page:
	// Acknowledge the interaction first (defer full response)
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
	})

	sendPaginatedResponse(s, i, pages, 0)
}

func buildPaginationContent(pages []string, page int) (string, []discordgo.MessageComponent) {
	maxPage := len(pages) - 1
	content := fmt.Sprintf("**Page %d / %d**\n\n%s", page+1, maxPage+1, pages[page])

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{CustomID: "first", Emoji: &discordgo.ComponentEmoji{Name: "⏮️"}, Style: discordgo.PrimaryButton},
				discordgo.Button{CustomID: "prev", Emoji: &discordgo.ComponentEmoji{Name: "⬅️"}, Style: discordgo.PrimaryButton},
				discordgo.Button{CustomID: "next", Emoji: &discordgo.ComponentEmoji{Name: "➡️"}, Style: discordgo.PrimaryButton},
				discordgo.Button{CustomID: "last", Emoji: &discordgo.ComponentEmoji{Name: "⏭️"}, Style: discordgo.PrimaryButton},
			},
		},
	}

	return content, components
}

func sendPaginatedResponse(s *discordgo.Session, ic *discordgo.InteractionCreate, pages []string, page int) {
	content, components := buildPaginationContent(pages, page)

	// Edit the original deferred reply (safer than follow‑up for first page)
	msg, err := s.InteractionResponseEdit(
		ic.Interaction,
		&discordgo.WebhookEdit{
			Content:    &content,
			Components: &components,
		},
	)
	if err != nil {
		log.Printf("pagination: initial edit failed: %v", err)
		return
	}

	// Store state by user ID + message ID for user-specific pagination
	// This prevents users from interfering with each other's pagination state
	userID := getUserID(ic)
	if userID != "" {
		key := getPaginationKey(userID, msg.ID)
		paginationStates[key] = PaginationState{
			Pages:     pages,
			Page:      page,
			CreatedAt: time.Now(),
		}

		// Clean up old pagination states (older than 1 hour) to prevent memory leaks
		cleanupOldPaginationStates()
	}
}

func handleSourceCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// Extract the mirror link from the slash command option
	mirrorLink := i.ApplicationCommandData().Options[0].StringValue()

	// Convert links from imgur.com to i.imgur.com + .mp4
	if strings.HasPrefix(mirrorLink, "https://imgur.com/") {
		mirrorLink = strings.Replace(mirrorLink, "https://imgur.com/", "https://i.imgur.com/", 1)
		mirrorLink += ".mp4"
	}

	// Use internal Pocketbase API instead of HTTP
	filter := fmt.Sprintf("mirror='%s'", strings.ReplaceAll(mirrorLink, "'", "\\'"))
	records, err := App.FindRecordsByFilter("v1", filter, "", 1, 0)

	if err != nil {
		respondWithError(s, i.Interaction, "Could not query database.")
		return
	}

	// Check if we got a record
	if len(records) == 0 {
		respondWithError(s, i.Interaction, "No matching record found for that mirror link.")
		return
	}

	record := records[0]
	sourceValue := record.GetString("source")

	// Decide which URL or link to respond with
	var content string
	if sourceValue != "" {
		content = fmt.Sprintf("🔗 Found source in KpopCat: %s", sourceValue)
	} else {
		content = "No source was available for that gif."
	}

	// Respond with the final link
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: content,
		},
	})
	if err != nil {
		log.Printf("Error responding to /source: %v", err)
	}
}

func handlePaginationInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	userID := getUserID(i)
	if userID == "" {
		respondWithError(s, i.Interaction, "Could not identify user.")
		return
	}

	messageID := i.Message.ID
	key := getPaginationKey(userID, messageID)

	state, ok := paginationStates[key]
	if !ok {
		respondWithError(s, i.Interaction, "Pagination state not found for this user.")
		return
	}

	switch i.MessageComponentData().CustomID {
	case "first":
		state.Page = 0
	case "prev":
		if state.Page > 0 {
			state.Page--
		}
	case "next":
		if state.Page < len(state.Pages)-1 {
			state.Page++
		}
	case "last":
		state.Page = len(state.Pages) - 1
	}

	paginationStates[key] = state

	content, components := buildPaginationContent(state.Pages, state.Page)

	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    content,
			Components: components,
		},
	})
	if err != nil {
		log.Printf("Error updating pagination message: %v", err)
	}
}

// cleanupOldPaginationStates removes pagination states older than 1 hour to prevent memory leaks
// This is called after each new pagination state is created to maintain reasonable memory usage
func cleanupOldPaginationStates() {
	cutoff := time.Now().Add(-1 * time.Hour)
	for key, state := range paginationStates {
		if state.CreatedAt.Before(cutoff) {
			delete(paginationStates, key)
		}
	}
}

// respondWithError is a helper function to unify error responses
func respondWithError(s *discordgo.Session, i *discordgo.Interaction, msg string) {
	_ = s.InteractionRespond(i, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
		},
	})
}

// RemoveCommands can be called from main.go to remove slash commands on shutdown.
func RemoveCommands(s *discordgo.Session) {
	for _, cmd := range registeredCommands {
		err := s.ApplicationCommandDelete(s.State.User.ID, "", cmd.ID)
		if err != nil {
			log.Printf("Cannot delete command %q: %v", cmd.Name, err)
		}
	}
}
