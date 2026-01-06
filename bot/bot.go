package bot

import (
	"fmt"
	"log/slog"
	"os"
	"path"
	"regexp"
	"strings"

	"kcat-v3-be/bot/utils"

	"github.com/bwmarrin/discordgo"
	"github.com/pocketbase/pocketbase"
)

// GLOBAL VARIABLES
var imgurRegexp = regexp.MustCompile(`https?://(i\.)?imgur\.com/([a-zA-Z0-9]+)(\.[a-zA-Z0-9]+)?`)
var roleRegexp = regexp.MustCompile(`(\w+) \[([^\]]+)\]`)
var youtubeRegexp = regexp.MustCompile(`(?:https?://)?(?:www\.)?(?:youtube\.com/watch\?v=|youtu\.be/)[\w\-]{11}`)
var pixeldrainRegexp = regexp.MustCompile(`(?:https?://)?(?:www\.)?pixeldrain\.com/(?:u|l)/[a-zA-Z0-9]+`)

var idolMap map[string][]IdolItem
var groupMap map[string]string
var uploaderMap map[string]string

var allowedChannelIDs = map[string]bool{
	"124767749099618304":  true,
	"1170632973389934612": true,
}

func initializeMappings() error {
	var err error

	// Load groups from database
	groupMap, err = loadGroupsFromDB()
	if err != nil {
		slog.Error("UNABLE TO LOAD GROUPS FROM DB: ", "MSG", err)
		return err
	}

	// Load uploaders from database
	uploaderMap, err = loadUploadersFromDB()
	if err != nil {
		slog.Error("UNABLE TO LOAD UPLOADERS FROM DB: ", "MSG", err)
		return err
	}

	// Load idols from database
	idolMap, err = loadIdolsFromDB()
	if err != nil {
		slog.Error("UNABLE TO LOAD IDOLS FROM DB: ", "MSG", err)
		return err
	}

	slog.Info("✅ Mappings initialized from database", "groups", len(groupMap), "uploaders", len(uploaderMap), "idols", len(idolMap))
	return nil
}

// Start initializes and starts the Discord bot
// It takes a Pocketbase app instance to use for internal database operations
func Start(pbApp *pocketbase.PocketBase) error {
	// Store the Pocketbase app globally for use in helpers
	App = pbApp

	token := os.Getenv("DISCORD_TOKEN")
	if token == "" {
		slog.Error("DISCORD_TOKEN environment variable is not set")
		return fmt.Errorf("DISCORD_TOKEN is required")
	}

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		slog.Error("UNABLE TO CREATE DISCORD SESSION: ", "MSG", err)
		return err
	}

	err = initializeMappings()
	if err != nil {
		slog.Error("UNABLE TO INITIALIZE MAPPINGS: ", "MSG", err)
		return err
	}

	dg.AddHandler(messageCreate)
	dg.AddHandler(commandUsed)

	err = dg.Open()
	if err != nil {
		slog.Error("UNABLE TO OPEN DISCORD SESSION", "MSG", err)
		return err
	}

	registerSlashCommands(dg)

	slog.Info("Discord bot is now running")
	return nil
}

func messageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	// Guard against malformed/unsupported events that may produce nil fields
	if m == nil || m.Message == nil || m.Author == nil {
		return
	}
	if m.Author.Bot {
		return
	}

	var imgurLinks []string
	metadata := Metadata{}
	isReply := false

	// bot reacts if its mentioned, there are roles pinged or if it's a reply
	// for ping roles and replies, we need to check if the channel is allowed
	if utils.BotIsMentioned(s, m) {
		imgurLinks = retrieveImgurLinks(m.Content)
		if len(imgurLinks) < 1 && len(m.Attachments) < 1 {
			return
		}
		err := extractMetadata(m.Content, &metadata)
		if err != nil {
			slog.Error("ERROR EXTRACTING METADATA", "MSG", err)
			return
		}
	} else if len(m.MentionRoles) > 0 && allowedChannelIDs[m.ChannelID] {
		imgurLinks = retrieveImgurLinks(m.Content)
		if len(imgurLinks) < 1 && len(m.Attachments) < 1 {
			return
		}

		pingRoleNames, err := getRoleNamesFromIDs(s, m.GuildID, m.MentionRoles)
		if err != nil {
			slog.Error("UNABLE TO GET ROLE NAMES", "MSG", err)
			return
		} else if len(pingRoleNames) < 1 {
			slog.Info("PING ROLES NOT FOUND", "MSG", pingRoleNames)
			return
		}

		metadata = createMetadata(pingRoleNames)
		err = extractMetadata(m.Content, &metadata)
		if err != nil {
			slog.Error("ERROR EXTRACTING METADATA", "MSG", err)
			return
		}
	} else if m.Message.ReferencedMessage != nil && allowedChannelIDs[m.ChannelID] {
		if m.Author.ID == lastMetadata.AuthorID && m.Message.ReferencedMessage.ID == lastMetadata.MessageID {
			imgurLinks = retrieveImgurLinks(m.Content)
			if len(imgurLinks) < 1 && len(m.Attachments) < 1 {
				return
			}
			isReply = true
			metadata = lastMetadata
		}
	} else {
		return
	}

	metadata.Uploader = m.Author.Username
	metadata.Discord = fmt.Sprintf("https://discord.com/channels/%s/%s/%s", m.GuildID, m.ChannelID, m.ID)
	totalItems := len(m.Attachments) + len(imgurLinks)

	if totalItems > 1 {
		if !isReply {
			metadata.SetId = utils.GenerateRandomString(15)

			err := createSetRecord(metadata)

			if err != nil {
				slog.Error("ERROR CREATING SET RECORD", "MSG", err)
				return
			}

			lastMetadata = metadata
			lastMetadata.AuthorID = m.Author.ID
			lastMetadata.MessageID = m.Message.ID
		}
	}

	// Now create each item in "contents"
	// 1) handle Discord attachments
	for _, attach := range m.Attachments {
		if strings.HasPrefix(attach.ContentType, "image/") {
			metadata.Filetype = "image"
		} else if strings.HasPrefix(attach.ContentType, "video/") {
			metadata.Filetype = "video"
		}
		_, err := processMediaLinks(attach.URL, attach.Filename, metadata)
		if err != nil {
			slog.Warn("unable to process media link (discord attach)", "MSG", err)
			continue
		}
	}

	// 2) handle Imgur links
	for _, imgurLink := range imgurLinks {
		metadata.Filetype = "video"
		filename := path.Base(imgurLink)
		metadata.Mirror = imgurLink

		_, err := processMediaLinks(imgurLink, filename, metadata)
		if err != nil {
			slog.Warn("unable to process media link (imgur)", "MSG", err)
			continue
		}
	}
}
