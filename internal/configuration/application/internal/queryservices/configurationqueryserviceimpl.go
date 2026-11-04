package queryservices

import (
	"hydrosmart/config/internal/configuration/domain/model/queries"
	"hydrosmart/config/internal/configuration/domain/services"
)

type ConfigurationQueryServiceImpl struct {
	Repository services.ConfigurationRepository
}

func (s ConfigurationQueryServiceImpl) Handle(q queries.GetConfigurationQuery) (map[string]string, bool) {
	return s.Repository.Get(q.Service, q.Profile)
}
