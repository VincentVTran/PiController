package config

type ApplicationConfig struct {
	Local struct {
		PiControllerDNS  string `json:"pi-controller-dns"`
		PiControllerPort string `json:"pi-controller-port"`
		DragonflyDbURL   string `json:"dragonflyDbURL"`
	} `json:"local"`
	Prod struct {
		PiControllerDNS  string `json:"pi-dns"`
		PiControllerPort string `json:"pi-controller-port"`
		DragonflyDbURL   string `json:"dragonflyDbURL"`
	} `json:"prod"`
}
