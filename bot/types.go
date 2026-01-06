package bot

import "time"

type Base struct {
	CollectionID   string    `json:"collectionId"`
	CollectionName string    `json:"collectionName"`
	ID             string    `json:"id"`
	Created        time.Time `json:"created"`
	Updated        time.Time `json:"updated"`
}

type Uploader struct {
	Base
	IsFeatured bool   `json:"isFeatured"`
	Name       string `json:"name"`
	User       string `json:"user"`
}

type Group struct {
	Base
	Code string `json:"code"`
	Name string `json:"name"`
}

type Idol struct {
	Base
	Code  string `json:"code"`
	Group string `json:"group"`
	Name  string `json:"name"`
}

type Metadata struct {
	MessageID     string   `json:"-"`
	AuthorID      string   `json:"-"`
	File          string   `json:"file"`
	Filetype      string   `json:"filetype"`
	Title         string   `json:"title"`
	Idol          string   `json:"idol"`
	Group         string   `json:"group"`
	Tags          string   `json:"tags"`
	Uploader      string   `json:"uploader"`
	Date          string   `json:"date"`
	Source        string   `json:"source"`
	Discord       string   `json:"discord"`
	Mirror        string   `json:"mirror"`
	HqMirror      string   `json:"hqMirror"`
	SetId         string   `json:"setId"`
	RecordIds     []string `json:"-"`
	SetResponseId string   `json:"-"`
}

type IdolItem struct {
	Code  string `json:"code"`
	Group string `json:"group"`
	ID    string `json:"id"`
	Name  string `json:"name"`
}

type IdolGroup struct {
	Idol  string
	Group string
}
