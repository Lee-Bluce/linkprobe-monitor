package api

import (
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"linkprobe/internal/store"
)

func (s *Server) listGroups(w http.ResponseWriter, r *http.Request) {
	groups, err := s.st.Groups(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"groups": groups})
}

type nameRequest struct {
	Name string `json:"name"`
}

func validName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 40 {
		return "", false
	}
	return name, true
}

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) {
	var req nameRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	name, ok := validName(req.Name)
	if !ok {
		writeError(w, http.StatusBadRequest, "invalid_name", "分组名称需为 1–40 个字符")
		return
	}
	g, err := s.st.CreateGroup(r.Context(), name, time.Now().Unix())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, g)
}

func (s *Server) renameGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	var req nameRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	name, valid := validName(req.Name)
	if !valid {
		writeError(w, http.StatusBadRequest, "invalid_name", "分组名称需为 1–40 个字符")
		return
	}
	if err := s.st.RenameGroup(r.Context(), id, name); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	if err := s.st.DeleteGroup(r.Context(), id); err != nil {
		s.fail(w, err)
		return
	}
	s.reloadScheduler(r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type orderRequest struct {
	IDs []int64 `json:"ids"`
}

func (s *Server) orderGroups(w http.ResponseWriter, r *http.Request) {
	var req orderRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := s.st.ReorderGroups(r.Context(), req.IDs); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	if err == store.ErrNotFound {
		writeError(w, http.StatusNotFound, "not_found", "对象不存在")
		return
	}
	s.log.Error("request failed", "err", err)
	writeError(w, http.StatusInternalServerError, "internal", "操作失败")
}
