// Package api defines the frozen HTTP and CLI JSON contract. Fields are add-only;
// new fields must be optional. Never serialize storage models or token hashes.
package api

import "github.com/reorx/tinyhost/internal/bundle"

type Error struct {
	Error ErrorDetail `json:"error"`
}
type ErrorDetail struct {
	Code         string   `json:"code"`
	Message      string   `json:"message"`
	Hint         string   `json:"hint"`
	CreatedBy    string   `json:"created_by,omitempty"`
	CreatedAt    *int64   `json:"created_at,omitempty"`
	Deleted      *bool    `json:"deleted,omitempty"`
	ExistingDirs []string `json:"existing_dirs,omitempty"`
}
type Client struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
	ExpiresAt *int64 `json:"expires_at"`
}
type WhoamiResult struct {
	Client        Client `json:"client"`
	ServerVersion string `json:"server_version"`
}
type App struct {
	ID             string `json:"id"`
	Slug           string `json:"slug"`
	Title          string `json:"title"`
	Visibility     string `json:"visibility"`
	CreatedBy      string `json:"created_by"`
	CurrentVersion *int   `json:"current_version"`
	CreatedAt      int64  `json:"created_at"`
	UpdatedAt      int64  `json:"updated_at"`
	MainURL        string `json:"main_url"`
}
type AppsResult struct {
	Apps []App `json:"apps"`
}
type Overlay struct {
	Dir      string `json:"dir"`
	PushedBy string `json:"pushed_by"`
	PushedAt int64  `json:"pushed_at"`
}
type AppDetail struct {
	App      App             `json:"app"`
	Manifest bundle.Manifest `json:"manifest"`
	Overlays []Overlay       `json:"overlays"`
}
type Version struct {
	Number    int     `json:"number"`
	Kind      string  `json:"kind"`
	DataDir   *string `json:"data_dir"`
	Message   string  `json:"message"`
	CreatedBy string  `json:"created_by"`
	CreatedAt int64   `json:"created_at"`
	Current   bool    `json:"current"`
}
type VersionsResult struct {
	Versions []Version `json:"versions"`
}
type PromoteRequest struct {
	Version int `json:"version"`
}
type PromoteResult struct {
	App            string `json:"app"`
	CurrentVersion int    `json:"current_version"`
	MainURL        string `json:"main_url"`
}
type DeleteResult struct {
	App       string `json:"app"`
	Deleted   bool   `json:"deleted"`
	TrashDays int    `json:"trash_days"`
}
type DeployResult struct {
	App            string   `json:"app"`
	Version        int      `json:"version"`
	Kind           string   `json:"kind"`
	DataDir        *string  `json:"data_dir"`
	Files          int      `json:"files"`
	Size           int64    `json:"size"`
	VersionURL     string   `json:"version_url"`
	MainURL        string   `json:"main_url"`
	Promoted       bool     `json:"promoted"`
	CurrentVersion *int     `json:"current_version"`
	AppCreatedBy   string   `json:"app_created_by"`
	Skipped        []string `json:"skipped"`
}
