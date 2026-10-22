package api

import (
	"maps"
	"sort"
	"time"

	"github.com/robbertmanders/beltline/daemon/internal/core"
	"github.com/robbertmanders/beltline/daemon/internal/integrations/store"
	"github.com/robbertmanders/beltline/daemon/internal/wire/jsonx"
)

// SettingsResponse is Settings as a transport contract. Persistence keeps sets
// in their original form; the wire format uses sorted arrays and explicit
// nulls for optionals.
//
// DataLocation and Demo are transient presentation facts, not persisted: where
// the daemon keeps its data and whether this launch is a demo. They belong on
// the read/patch response, never in the stored Settings.
type SettingsResponse struct {
	Login                string                   `json:"login"`
	Notifications        bool                     `json:"notifications"`
	LibraryRepositories  []core.LibraryRepository `json:"libraryRepositories"`
	ConfigurationBackend core.Backend             `json:"configurationBackend"`
	ConfigurationModel   string                   `json:"configurationModel"`
	// ConfigurationThinking is written as an explicit null when unset.
	ConfigurationThinking *core.Thinking `json:"configurationThinking"`
	Paused                bool           `json:"paused"`
	// PausedAt is written as an explicit null when the belts are not paused.
	PausedAt      *jsonx.Date                          `json:"pausedAt"`
	Pauses        []core.DateInterval                  `json:"pauses"`
	AllowedModels map[string][]string                  `json:"allowedModels"`
	ModelCatalogs map[string]core.ModelCatalogSnapshot `json:"modelCatalogs"`
	Afk           core.AfkSettings                     `json:"afk"`
	BeltLook      core.BeltLook                        `json:"beltLook"`
	ShowInsights  bool                                 `json:"showInsights"`
	DataLocation  string                               `json:"dataLocation"`
	Demo          bool                                 `json:"demo"`
}

// NewSettingsResponse builds the settings response from its arguments.
// Allowed models are sorted per runner, as the initialiser does.
func NewSettingsResponse(settings store.Settings, dataLocation string, demo bool) SettingsResponse {
	thinking := core.CopyPointer(settings.ConfigurationThinking)
	pausedAt := core.CopyPointer(settings.PausedAt)
	pauses := make([]core.DateInterval, 0, len(settings.Pauses))
	for _, pause := range settings.Pauses {
		pauses = append(pauses, settingsCoreInterval(pause))
	}
	allowed := make(map[string][]string, len(settings.AllowedModels))
	for runner, set := range settings.AllowedModels {
		identifiers := make([]string, 0, len(set))
		for identifier := range set {
			identifiers = append(identifiers, identifier)
		}
		sort.Strings(identifiers)
		allowed[runner] = identifiers
	}
	catalogs := make(map[string]core.ModelCatalogSnapshot, len(settings.ModelCatalogs))
	maps.Copy(catalogs, settings.ModelCatalogs)
	return SettingsResponse{
		Login:                 settings.Login,
		Notifications:         settings.Notifications,
		LibraryRepositories:   settings.LibraryRepositories,
		ConfigurationBackend:  settings.ConfigurationBackend,
		ConfigurationModel:    settings.ConfigurationModel,
		ConfigurationThinking: thinking,
		Paused:                settings.Paused,
		PausedAt:              pausedAt,
		Pauses:                pauses,
		AllowedModels:         allowed,
		ModelCatalogs:         catalogs,
		Afk:                   settings.Afk,
		BeltLook:              settings.BeltLook,
		ShowInsights:          settings.ShowInsights,
		DataLocation:          dataLocation,
		Demo:                  demo,
	}
}

// settingsCoreInterval turns Foundation's stored form of a pause (its start and
// its duration in seconds) into the interval the presentation uses.
func settingsCoreInterval(pause store.DateInterval) core.DateInterval {
	start := pause.Start.Time
	return core.DateInterval{
		Start: start,
		End:   start.Add(time.Duration(pause.Duration * float64(time.Second))),
	}
}

// settingsPauseWire is a pause as Foundation's Codable writes a DateInterval:
// its start and its duration in seconds.
type settingsPauseWire struct {
	Start    jsonx.Date `json:"start"`
	Duration float64    `json:"duration"`
}

// settingsResponseWire is SettingsResponse with the pauses in their wire form.
// core.DateInterval holds time.Time values, which jsonx does not encode.
type settingsResponseWire struct {
	Login                 string                               `json:"login"`
	Notifications         bool                                 `json:"notifications"`
	LibraryRepositories   []core.LibraryRepository             `json:"libraryRepositories"`
	ConfigurationBackend  core.Backend                         `json:"configurationBackend"`
	ConfigurationModel    string                               `json:"configurationModel"`
	ConfigurationThinking *core.Thinking                       `json:"configurationThinking"`
	Paused                bool                                 `json:"paused"`
	PausedAt              *jsonx.Date                          `json:"pausedAt"`
	Pauses                []settingsPauseWire                  `json:"pauses"`
	AllowedModels         map[string][]string                  `json:"allowedModels"`
	ModelCatalogs         map[string]core.ModelCatalogSnapshot `json:"modelCatalogs"`
	Afk                   core.AfkSettings                     `json:"afk"`
	BeltLook              core.BeltLook                        `json:"beltLook"`
	ShowInsights          bool                                 `json:"showInsights"`
	DataLocation          string                               `json:"dataLocation"`
	Demo                  bool                                 `json:"demo"`
}

// MarshalJSON writes the value as JSON.
func (s SettingsResponse) MarshalJSON() ([]byte, error) {
	pauses := make([]settingsPauseWire, 0, len(s.Pauses))
	for _, pause := range s.Pauses {
		pauses = append(pauses, settingsPauseWire{
			Start:    jsonx.Date{Time: pause.Start},
			Duration: pause.Duration(),
		})
	}
	return jsonx.Marshal(settingsResponseWire{
		Login:                 s.Login,
		Notifications:         s.Notifications,
		LibraryRepositories:   s.LibraryRepositories,
		ConfigurationBackend:  s.ConfigurationBackend,
		ConfigurationModel:    s.ConfigurationModel,
		ConfigurationThinking: s.ConfigurationThinking,
		Paused:                s.Paused,
		PausedAt:              s.PausedAt,
		Pauses:                pauses,
		AllowedModels:         s.AllowedModels,
		ModelCatalogs:         s.ModelCatalogs,
		Afk:                   s.Afk,
		BeltLook:              s.BeltLook,
		ShowInsights:          s.ShowInsights,
		DataLocation:          s.DataLocation,
		Demo:                  s.Demo,
	})
}
