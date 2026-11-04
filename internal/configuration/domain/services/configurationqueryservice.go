package services

import "hydrosmart/config/internal/configuration/domain/model/queries"

type ConfigurationQueryService interface {
	Handle(queries.GetConfigurationQuery) (map[string]string, bool)
}
