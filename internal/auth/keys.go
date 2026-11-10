package auth

import (
	"net/http"
)

// RenameKey renames the key id.
func (s *Service) RenameKey(id, name string) error {
	name, err := ValidName(name)
	if err != nil {
		return err
	}
	if _, ok, err := s.Creds.Get(id); err != nil {
		return err
	} else if !ok {
		return fail(http.StatusNotFound, "key_not_found", "That key isn't added")
	}
	if taken, err := s.Creds.NameTaken(name, id); err != nil {
		return err
	} else if taken {
		return fail(http.StatusBadRequest, "name_taken", "Another key already has that name")
	}
	_, err = s.Creds.Rename(id, name)
	return err
}

// DeleteKey removes the key id. The last key can't be removed while the panel
// needs a key to sign in.
func (s *Service) DeleteKey(id string) error {
	if _, ok, err := s.Creds.Get(id); err != nil {
		return err
	} else if !ok {
		return fail(http.StatusNotFound, "key_not_found", "That key isn't added")
	}
	n, err := s.Creds.Count()
	if err != nil {
		return err
	}
	if n == 1 && s.KeyRequired() {
		return fail(http.StatusConflict, "last_key", "Add another key before removing this one")
	}
	_, err = s.Creds.Delete(id)
	return err
}
