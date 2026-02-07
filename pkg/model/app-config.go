package config

type ApplicationConfig struct {
	Local struct {
		RabbitMQLink   string `json:"rabbitMQLink"`
		Exchange       string `json:"exchange"`
		RoutingKey     string `json:"routingKey"`
		DragonflyDbURL string `json:"dragonflyDbURL"`
	} `json:"local"`
	Prod struct {
		RabbitMQLink   string `json:"rabbitMQLink"`
		Exchange       string `json:"exchange"`
		RoutingKey     string `json:"routingKey"`
		PiDNS          string `json:"pi-dns"`
		DragonflyDbURL string `json:"dragonflyDbURL"`
	} `json:"prod"`
}
