package services

type ConfigurationRepository interface {
	Get(string, string) (map[string]string, bool)
}
