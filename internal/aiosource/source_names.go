package aiosource

import "strings"

func (s *Service) sourceNames() map[string]string {
	names := map[string]string{}
	if s.sources != nil {
		if sources, err := s.sources.List(); err == nil {
			for _, source := range sources {
				if name := strings.TrimSpace(source.Name); name != "" {
					names[source.ID] = name
				}
			}
		}
	}
	return names
}
