package api

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (sm *SupervisorMux) serveCitySessionAsset(w http.ResponseWriter, r *http.Request) {
	srv := sm.resolveCityServer(r.PathValue("cityName"))
	if srv == nil {
		problemCityNotFound.writeTo(w)
		return
	}
	srv.handleSessionAssetServe(w, r, r.PathValue("id"), r.URL.Query().Get("path"))
}

func (s *Server) handleSessionAssetServe(w http.ResponseWriter, r *http.Request, idRef, rawPath string) {
	store := s.state.CityBeadStore()
	if store == nil {
		writeError(w, http.StatusServiceUnavailable, "unavailable", "no bead store configured")
		return
	}
	sessionID, err := s.resolveSessionIDAllowClosedWithConfig(store, idRef)
	if err != nil {
		writeHumaStatusError(w, humaResolveError(err))
		return
	}

	info, err := s.sessionManager(store).Get(sessionID)
	if err != nil {
		writeHumaStatusError(w, humaSessionManagerError(err))
		return
	}
	path, err := resolveSessionAssetPath(info.WorkDir, rawPath)
	if err != nil {
		writeSessionAssetError(w, err)
		return
	}
	if err := serveSessionAssetFile(w, r, path); err != nil {
		writeSessionAssetError(w, err)
		return
	}
}

type sessionResolvedAssetPath struct {
	path        string
	allowedRoot string
}

func resolveSessionAssetPath(workDir, rawPath string) (sessionResolvedAssetPath, error) {
	workDir = strings.TrimSpace(workDir)
	if workDir == "" {
		return sessionResolvedAssetPath{}, sessionAssetClientError{status: http.StatusNotFound, code: "work_dir_missing", message: "session work_dir is not available"}
	}
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return sessionResolvedAssetPath{}, sessionAssetClientError{status: http.StatusBadRequest, code: "path_required", message: "path query parameter is required"}
	}
	if strings.ContainsRune(rawPath, 0) || strings.HasPrefix(strings.ToLower(rawPath), "file://") {
		return sessionResolvedAssetPath{}, sessionAssetClientError{status: http.StatusBadRequest, code: "invalid_path", message: "invalid asset path"}
	}

	workDirAbs, err := filepath.Abs(workDir)
	if err != nil {
		return sessionResolvedAssetPath{}, sessionAssetClientError{status: http.StatusBadRequest, code: "invalid_work_dir", message: "invalid session work_dir"}
	}
	workDirEval, err := filepath.EvalSymlinks(workDirAbs)
	if err != nil {
		return sessionResolvedAssetPath{}, sessionAssetClientError{status: http.StatusNotFound, code: "work_dir_missing", message: "session work_dir is not available"}
	}
	workDirInfo, err := os.Stat(workDirEval)
	if err != nil || !workDirInfo.IsDir() {
		return sessionResolvedAssetPath{}, sessionAssetClientError{status: http.StatusNotFound, code: "work_dir_missing", message: "session work_dir is not available"}
	}

	target := rawPath
	if !filepath.IsAbs(target) {
		target = filepath.Join(workDirAbs, target)
	}
	targetAbs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return sessionResolvedAssetPath{}, sessionAssetClientError{status: http.StatusBadRequest, code: "invalid_path", message: "invalid asset path"}
	}
	if !pathWithinDir(workDirAbs, targetAbs) && !pathWithinDir(workDirEval, targetAbs) {
		return sessionResolvedAssetPath{}, sessionAssetClientError{status: http.StatusForbidden, code: "path_forbidden", message: "asset path must stay inside session work_dir"}
	}

	targetEval, err := filepath.EvalSymlinks(targetAbs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return sessionResolvedAssetPath{}, sessionAssetClientError{status: http.StatusNotFound, code: "not_found", message: "asset not found"}
		}
		return sessionResolvedAssetPath{}, err
	}
	if !pathWithinDir(workDirEval, targetEval) {
		return sessionResolvedAssetPath{}, sessionAssetClientError{status: http.StatusForbidden, code: "path_forbidden", message: "asset path must stay inside session work_dir"}
	}
	return sessionResolvedAssetPath{path: targetEval, allowedRoot: workDirEval}, nil
}

func serveSessionAssetFile(w http.ResponseWriter, r *http.Request, resolved sessionResolvedAssetPath) error {
	file, err := os.Open(resolved.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return sessionAssetClientError{status: http.StatusNotFound, code: "not_found", message: "asset not found"}
		}
		if errors.Is(err, os.ErrPermission) {
			return sessionAssetClientError{status: http.StatusForbidden, code: "forbidden", message: "asset is not readable"}
		}
		return err
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil {
		return err
	}
	if err := validateOpenSessionAssetFile(info, resolved); err != nil {
		return err
	}
	if info.IsDir() {
		return sessionAssetClientError{status: http.StatusNotFound, code: "not_found", message: "asset not found"}
	}
	if info.Size() > sessionAttachmentMaxBytes {
		return sessionAssetClientError{status: http.StatusRequestEntityTooLarge, code: "too_large", message: fmt.Sprintf("image assets are limited to %d MB", sessionAttachmentMaxBytes>>20)}
	}

	peek := make([]byte, 512)
	n, readErr := file.Read(peek)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}
	mimeType := strings.ToLower(http.DetectContentType(peek[:n]))
	if !isAllowedImageMime(mimeType) {
		return sessionAssetClientError{status: http.StatusUnsupportedMediaType, code: "unsupported_media_type", message: "only image assets are supported"}
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", inlineContentDisposition(filepath.Base(resolved.path)))
	http.ServeContent(w, r, filepath.Base(resolved.path), info.ModTime(), file)
	return nil
}

func validateOpenSessionAssetFile(info os.FileInfo, resolved sessionResolvedAssetPath) error {
	allowedRoot := strings.TrimSpace(resolved.allowedRoot)
	if allowedRoot == "" {
		return sessionAssetClientError{status: http.StatusForbidden, code: "path_forbidden", message: "asset path must stay inside its allowed root"}
	}
	currentTarget, err := filepath.EvalSymlinks(resolved.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return sessionAssetClientError{status: http.StatusNotFound, code: "not_found", message: "asset not found"}
		}
		return err
	}
	if !pathWithinDir(allowedRoot, currentTarget) {
		return sessionAssetClientError{status: http.StatusForbidden, code: "path_forbidden", message: "asset path must stay inside its allowed root"}
	}
	currentInfo, err := os.Stat(currentTarget)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return sessionAssetClientError{status: http.StatusNotFound, code: "not_found", message: "asset not found"}
		}
		if errors.Is(err, os.ErrPermission) {
			return sessionAssetClientError{status: http.StatusForbidden, code: "forbidden", message: "asset is not readable"}
		}
		return err
	}
	if !os.SameFile(info, currentInfo) {
		return sessionAssetClientError{status: http.StatusForbidden, code: "path_forbidden", message: "asset path changed during validation"}
	}
	return nil
}

func inlineContentDisposition(filename string) string {
	filename = strings.Map(func(r rune) rune {
		switch r {
		case 0, '\r', '\n':
			return -1
		default:
			return r
		}
	}, strings.TrimSpace(filename))
	if filename == "" {
		return "inline"
	}
	if value := mime.FormatMediaType("inline", map[string]string{"filename": filename}); value != "" {
		return value
	}
	return "inline"
}

func pathWithinDir(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel))
}

type sessionAssetClientError struct {
	status  int
	code    string
	message string
}

func (e sessionAssetClientError) Error() string {
	return e.message
}

func writeSessionAssetError(w http.ResponseWriter, err error) {
	var clientErr sessionAssetClientError
	if errors.As(err, &clientErr) {
		writeError(w, clientErr.status, clientErr.code, clientErr.message)
		return
	}
	writeError(w, http.StatusInternalServerError, "internal", err.Error())
}
