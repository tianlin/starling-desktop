package model

// CloudProgress is a platform playback record. An absent record differs from a
// record at zero seconds; PlayedAt is preserved when sending a queued update.
type CloudProgress struct {
	EpisodeID string  `json:"eid"`
	PodcastID string  `json:"pid"`
	Position  float64 `json:"progress"`
	PlayedAt  string  `json:"playedAt"`
}
