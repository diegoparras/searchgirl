package searx

import "encoding/json"

// rawResponse mirrors the JSON that SearXNG emits with format=json. Fields
// whose shape changed across SearXNG releases (answers, corrections,
// unresponsive_engines) are decoded leniently as raw messages and interpreted
// in normalize.go.
type rawResponse struct {
	Query               string            `json:"query"`
	NumberOfResults     float64           `json:"number_of_results"`
	Results             []rawResult       `json:"results"`
	Answers             []json.RawMessage `json:"answers"`
	Corrections         []json.RawMessage `json:"corrections"`
	Infoboxes           []rawInfobox      `json:"infoboxes"`
	Suggestions         []string          `json:"suggestions"`
	UnresponsiveEngines []json.RawMessage `json:"unresponsive_engines"`
}

type rawResult struct {
	Title         string   `json:"title"`
	URL           string   `json:"url"`
	Content       string   `json:"content"`
	Engine        string   `json:"engine"`
	Engines       []string `json:"engines"`
	Score         float64  `json:"score"`
	Category      string   `json:"category"`
	PublishedDate string   `json:"publishedDate"`
	Thumbnail     string   `json:"thumbnail"`
	ThumbnailSrc  string   `json:"thumbnail_src"`
	ImgSrc        string   `json:"img_src"`
}

type rawInfobox struct {
	Infobox    string     `json:"infobox"` // the title
	ID         string     `json:"id"`
	Content    string     `json:"content"`
	ImgSrc     string     `json:"img_src"`
	URLs       []rawIBURL `json:"urls"`
	Engine     string     `json:"engine"`
	Attributes []rawIBAtt `json:"attributes"`
}

type rawIBURL struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type rawIBAtt struct {
	Label string `json:"label"`
	Value string `json:"value"`
}
