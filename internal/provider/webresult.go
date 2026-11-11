package provider

// WebAnswer is what a provider's web search or page question came back with.
type WebAnswer struct {
	Text    string      `json:"text"`
	Sources []WebSource `json:"sources,omitempty"`
}

// WebSource is one page the answer drew on.
type WebSource struct {
	Title string `json:"title,omitempty"`
	URL   string `json:"url"`
}

// AddSource appends a source unless its URL is already listed.
func (a *WebAnswer) AddSource(title, url string) {
	if url == "" {
		return
	}
	for _, s := range a.Sources {
		if s.URL == url {
			return
		}
	}
	a.Sources = append(a.Sources, WebSource{Title: title, URL: url})
}
