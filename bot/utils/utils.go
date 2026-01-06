package utils

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

const charset = "abcdefghijklmnopqrstuvwxyz0123456789"

func GenerateRandomString(length int) string {
	// Create a new source and rand
	src := rand.NewSource(time.Now().UnixNano())
	rnd := rand.New(src)

	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rnd.Intn(len(charset))]
	}
	return string(b)
}

func BotIsMentioned(s *discordgo.Session, m *discordgo.MessageCreate) bool {
	botID := s.State.User.ID

	userMention := fmt.Sprintf("<@%s>", botID)
	userMentionWithNick := fmt.Sprintf("<@!%s>", botID)

	if strings.Contains(m.Content, userMention) || strings.Contains(m.Content, userMentionWithNick) {
		return true
	}

	return false
}

func GenerateLinkFromFilename(recordID string, filename string) string {
	link := fmt.Sprintf("https://kcat.pics/v1/%s/%s", recordID, filename)
	return link
}
