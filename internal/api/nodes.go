package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"linkprobe/internal/probe"
	"linkprobe/internal/store"
)

var hostnameRE = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(\.[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*\.?$`)

func validateHost(host string) error {
	if host == "" {
		return fmt.Errorf("主机不能为空")
	}
	if len(host) > 253 {
		return fmt.Errorf("主机名过长")
	}
	if net.ParseIP(host) != nil {
		return nil
	}
	if !hostnameRE.MatchString(host) {
		return fmt.Errorf("主机名不合法")
	}
	return nil
}

func validateNode(n *store.Node) error {
	n.Name = strings.TrimSpace(n.Name)
	n.Host = strings.TrimSpace(n.Host)
	n.Note = strings.TrimSpace(n.Note)
	if n.Name == "" || utf8.RuneCountInString(n.Name) > 64 {
		return fmt.Errorf("名称需为 1–64 个字符")
	}
	if n.Type != "icmp" && n.Type != "tcp" {
		return fmt.Errorf("探测方式必须是 icmp 或 tcp")
	}
	if err := validateHost(n.Host); err != nil {
		return err
	}
	if n.Type == "tcp" {
		if n.Port < 1 || n.Port > 65535 {
			return fmt.Errorf("端口需在 1–65535 之间")
		}
	} else {
		n.Port = 0
	}
	if n.IntervalS < 10 || n.IntervalS > 300 {
		return fmt.Errorf("间隔需在 10–300 秒之间")
	}
	if n.Count < 3 || n.Count > 10 {
		return fmt.Errorf("每轮包数需在 3–10 之间")
	}
	if n.TimeoutMs < 1000 || n.TimeoutMs > 5000 {
		return fmt.Errorf("超时需在 1000–5000 毫秒之间")
	}
	switch n.Family {
	case "", "auto":
		n.Family = "auto"
	case "4", "6":
	default:
		return fmt.Errorf("协议族必须是 auto、4 或 6")
	}
	if utf8.RuneCountInString(n.Note) > 200 {
		return fmt.Errorf("备注最多 200 个字符")
	}
	if n.IntervalS < 30 && n.TimeoutMs > 2000 {
		// A round would not fit in its own interval.
		return fmt.Errorf("间隔较短时请把超时降到 2000 毫秒以内")
	}
	return nil
}

type nodeInput struct {
	GroupID   *int64 `json:"group_id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Host      string `json:"host"`
	Port      int    `json:"port"`
	IntervalS *int   `json:"interval_s"`
	Count     *int   `json:"count"`
	TimeoutMs *int   `json:"timeout_ms"`
	Family    string `json:"family"`
	Enabled   *bool  `json:"enabled"`
	Note      string `json:"note"`
}

func (in nodeInput) node() store.Node {
	n := store.Node{
		GroupID:   in.GroupID,
		Name:      in.Name,
		Type:      in.Type,
		Host:      in.Host,
		Port:      in.Port,
		IntervalS: 30,
		Count:     5,
		TimeoutMs: 2000,
		Family:    "auto",
		Enabled:   true,
		Note:      in.Note,
	}
	if in.IntervalS != nil {
		n.IntervalS = *in.IntervalS
	}
	if in.Count != nil {
		n.Count = *in.Count
	}
	if in.TimeoutMs != nil {
		n.TimeoutMs = *in.TimeoutMs
	}
	if in.Family != "" {
		n.Family = in.Family
	}
	if in.Enabled != nil {
		n.Enabled = *in.Enabled
	}
	return n
}

func (s *Server) groupExists(ctx context.Context, id *int64) (bool, error) {
	if id == nil {
		return true, nil
	}
	groups, err := s.st.Groups(ctx)
	if err != nil {
		return false, err
	}
	for _, g := range groups {
		if g.ID == *id {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) listNodes(w http.ResponseWriter, r *http.Request) {
	nodes, err := s.st.Nodes(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
}

func (s *Server) createNode(w http.ResponseWriter, r *http.Request) {
	var in nodeInput
	if err := decodeBody(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	n := in.node()
	if err := validateNode(&n); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_node", err.Error())
		return
	}
	if ok, err := s.groupExists(r.Context(), n.GroupID); err != nil {
		s.fail(w, err)
		return
	} else if !ok {
		writeError(w, http.StatusBadRequest, "invalid_group", "分组不存在")
		return
	}
	n, err := s.st.CreateNode(r.Context(), n, time.Now().Unix())
	if err != nil {
		s.fail(w, err)
		return
	}
	s.reloadScheduler(r)
	writeJSON(w, http.StatusCreated, n)
}

func (s *Server) getNode(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	n, err := s.st.Node(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, n)
}

func (s *Server) patchNode(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	var raw map[string]json.RawMessage
	if err := decodeBody(w, r, &raw); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	node, err := s.st.UpdateNode(r.Context(), id, func(n *store.Node) error {
		for key, val := range raw {
			switch key {
			case "group_id":
				if string(val) == "null" {
					n.GroupID = nil
					continue
				}
				var gid int64
				if err := json.Unmarshal(val, &gid); err != nil {
					return fmt.Errorf("分组编号无效")
				}
				n.GroupID = &gid
			case "name":
				if err := json.Unmarshal(val, &n.Name); err != nil {
					return fmt.Errorf("名称无效")
				}
			case "type":
				if err := json.Unmarshal(val, &n.Type); err != nil {
					return fmt.Errorf("探测方式无效")
				}
			case "host":
				if err := json.Unmarshal(val, &n.Host); err != nil {
					return fmt.Errorf("主机无效")
				}
			case "port":
				if err := json.Unmarshal(val, &n.Port); err != nil {
					return fmt.Errorf("端口无效")
				}
			case "interval_s":
				if err := json.Unmarshal(val, &n.IntervalS); err != nil {
					return fmt.Errorf("间隔无效")
				}
			case "count":
				if err := json.Unmarshal(val, &n.Count); err != nil {
					return fmt.Errorf("包数无效")
				}
			case "timeout_ms":
				if err := json.Unmarshal(val, &n.TimeoutMs); err != nil {
					return fmt.Errorf("超时无效")
				}
			case "family":
				if err := json.Unmarshal(val, &n.Family); err != nil {
					return fmt.Errorf("协议族无效")
				}
			case "enabled":
				if err := json.Unmarshal(val, &n.Enabled); err != nil {
					return fmt.Errorf("启用状态无效")
				}
			case "note":
				if err := json.Unmarshal(val, &n.Note); err != nil {
					return fmt.Errorf("备注无效")
				}
			}
		}
		return validateNode(n)
	})
	if err != nil {
		if err == store.ErrNotFound {
			s.fail(w, err)
			return
		}
		writeError(w, http.StatusBadRequest, "invalid_node", err.Error())
		return
	}
	if ok, err := s.groupExists(r.Context(), node.GroupID); err != nil {
		s.fail(w, err)
		return
	} else if !ok {
		writeError(w, http.StatusBadRequest, "invalid_group", "分组不存在")
		return
	}
	s.reloadScheduler(r)
	writeJSON(w, http.StatusOK, node)
}

func (s *Server) deleteNode(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	if err := s.st.DeleteNode(r.Context(), id); err != nil {
		s.fail(w, err)
		return
	}
	s.reloadScheduler(r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

type nodeOrderRequest struct {
	Items []store.NodeOrderItem `json:"items"`
}

func (s *Server) orderNodes(w http.ResponseWriter, r *http.Request) {
	var req nodeOrderRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if err := s.st.ReorderNodes(r.Context(), req.Items); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func probeResultJSON(res probe.Result, elapsed time.Duration) map[string]any {
	out := map[string]any{
		"sent":       res.Sent,
		"recv":       res.Recv,
		"loss":       res.Loss(),
		"elapsed_ms": elapsed.Milliseconds(),
	}
	if res.Recv > 0 {
		out["rtt_min_ms"] = usToMs(res.RttMinUs)
		out["rtt_avg_ms"] = usToMs(res.RttAvgUs)
		out["rtt_max_ms"] = usToMs(res.RttMaxUs)
	}
	if res.Reason != "" {
		out["reason"] = res.Reason
	}
	return out
}

func usToMs(us int64) float64 { return float64(us) / 1000 }

func (s *Server) testNode(w http.ResponseWriter, r *http.Request) {
	id, ok := s.pathID(w, r)
	if !ok {
		return
	}
	n, err := s.st.Node(r.Context(), id)
	if err != nil {
		s.fail(w, err)
		return
	}
	res, elapsed := s.sched.ProbeNow(r.Context(), n)
	writeJSON(w, http.StatusOK, probeResultJSON(res, elapsed))
}

func (s *Server) testDraft(w http.ResponseWriter, r *http.Request) {
	var in nodeInput
	if err := decodeBody(w, r, &in); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	n := in.node()
	// A draft test is usually run before the name is filled in.
	if strings.TrimSpace(n.Name) == "" {
		n.Name = "测试"
	}
	if err := validateNode(&n); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_node", err.Error())
		return
	}
	res, elapsed := s.sched.ProbeNow(r.Context(), n)
	writeJSON(w, http.StatusOK, probeResultJSON(res, elapsed))
}

func (s *Server) reloadScheduler(r *http.Request) {
	nodes, err := s.st.Nodes(r.Context())
	if err != nil {
		s.log.Error("reload scheduler failed", "err", err)
		return
	}
	s.sched.Reload(nodes)
}
