package application

import (
	"hydrosmart/config/internal/configuration/application/internal/queryservices"
	"hydrosmart/config/internal/configuration/domain/services"
)

func NewQueries(repository services.ConfigurationRepository) services.ConfigurationQueryService {
	return queryservices.ConfigurationQueryServiceImpl{Repository: repository}
}
