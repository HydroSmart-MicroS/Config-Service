package transform

import "hydrosmart/config/internal/configuration/interfaces/rest/resources"

func ToResource(values map[string]string) resources.ConfigurationResource {
	result := resources.ConfigurationResource{}
	for key, value := range values {
		result[key] = value
	}
	return result
}
