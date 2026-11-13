package main

import (
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
)

func iservFlySecretLines(sec secrets, encodedUser bool) ([]string, error) {
	obscured, err := obscureRclonePassword(sec.IServPassword)
	if err != nil {
		return nil, err
	}
	// flyctl's dotenv reader treats # and leading quotes as syntax. An
	// encoded value keeps the tested username intact through that parser.
	user := "RCLONE_CONFIG_ISERV_USER=" + sec.IServUser
	if encodedUser {
		user = "ISERV_WEBDAV_USER_B64=" + base64.StdEncoding.EncodeToString([]byte(sec.IServUser))
	}
	return []string{
		"RCLONE_CONFIG_ISERV_TYPE=webdav",
		"RCLONE_CONFIG_ISERV_URL=" + sec.IServURL,
		"RCLONE_CONFIG_ISERV_VENDOR=other",
		user,
		"RCLONE_CONFIG_ISERV_PASS=" + obscured,
	}, nil
}

type davMultistatus struct {
	Responses []davResponse `xml:"response"`
}
type davResponse struct {
	Href      string `xml:"href"`
	Propstats []struct {
		Prop struct {
			ResourceType struct {
				Collection *struct{} `xml:"collection"`
			} `xml:"resourcetype"`
		} `xml:"prop"`
	} `xml:"propstat"`
}

func validDAVRemotePath(remotePath string) bool {
	if len(remotePath) > 2000 || strings.ContainsAny(remotePath, "\\\x00\r\n") {
		return false
	}
	for _, part := range strings.Split(strings.Trim(remotePath, "/"), "/") {
		if part == "." || part == ".." {
			return false
		}
	}
	return true
}

func webdavURL(baseURL, remotePath string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("ungültige WebDAV-Adresse")
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return "", fmt.Errorf("WebDAV-Adresse muss HTTPS verwenden")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("ungültige WebDAV-Adresse")
	}
	if !validDAVRemotePath(remotePath) {
		return "", fmt.Errorf("ungültiger WebDAV-Ordnerpfad")
	}
	clean := strings.Trim(remotePath, "/")
	if clean != "" {
		u.Path = path.Join(strings.TrimSuffix(u.Path, "/"), clean)
	}
	return u.String(), nil
}

func normalizeDAVPath(p string) string {
	if p == "" {
		return "/"
	}
	clean := path.Clean("/" + strings.TrimPrefix(p, "/"))
	if clean == "." || clean == "" {
		return "/"
	}
	return clean
}

// decodeWebDAVDirectories reads a Depth:1 multistatus response and returns only
// child collections. target is the exact URL that was queried. Comparing against
// the target URL (rather than just remotePath) is important for valid IServ URLs
// such as https://schule.example/webdav.
func decodeWebDAVDirectories(target, remotePath string, r io.Reader) ([]string, error) {
	var ms davMultistatus
	if err := xml.NewDecoder(r).Decode(&ms); err != nil {
		return nil, fmt.Errorf("WebDAV-Antwort konnte nicht gelesen werden: %w", err)
	}

	targetURL, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("WebDAV-Ziel konnte nicht ausgewertet werden")
	}
	targetPath := normalizeDAVPath(targetURL.Path)
	targetURL.Path = strings.TrimRight(targetURL.Path, "/") + "/"
	targetURL.RawPath = ""

	seen := map[string]bool{}
	var dirs []string
	for _, item := range ms.Responses {
		isDir := false
		for _, ps := range item.Propstats {
			if ps.Prop.ResourceType.Collection != nil {
				isDir = true
				break
			}
		}
		if !isDir {
			continue
		}

		ref, err := url.Parse(item.Href)
		if err != nil {
			continue
		}
		resolved := targetURL.ResolveReference(ref)
		if !strings.EqualFold(resolved.Scheme, targetURL.Scheme) || !strings.EqualFold(resolved.Host, targetURL.Host) {
			continue
		}
		responsePath := normalizeDAVPath(resolved.Path)
		if responsePath == targetPath || path.Dir(responsePath) != targetPath {
			continue
		}

		name := path.Base(strings.TrimSuffix(responsePath, "/"))
		if name == "." || name == "/" || name == "" || strings.ContainsAny(name, "\\\x00\r\n") {
			continue
		}
		child := strings.Trim(strings.Trim(remotePath, "/")+"/"+name, "/")
		if child != "" && !seen[child] {
			seen[child] = true
			dirs = append(dirs, child)
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

func webDAVStatusError(status int, body, remotePath string) error {
	where := "/" + strings.Trim(remotePath, "/")
	if where == "/" {
		where = "Startordner"
	} else {
		where = "Ordner " + where
	}

	lower := strings.ToLower(body)
	permissionDenied := status == http.StatusForbidden ||
		strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "sudoruntimeexception") ||
		strings.Contains(lower, "access denied")

	switch {
	case status == http.StatusUnauthorized:
		return fmt.Errorf("IServ lehnt die Anmeldung ab (HTTP 401). Bitte Benutzername und Passwort bzw. App-Passwort prüfen")
	case permissionDenied:
		return fmt.Errorf("%s ist über WebDAV nicht zugänglich. IServ listet den Ordner zwar, verweigert aber das Öffnen. Wähle einen anderen Ordner; falls du diesen Ordner in IServ selbst öffnen kannst, ist seine WebDAV-Berechtigung wahrscheinlich eingeschränkt", where)
	default:
		return fmt.Errorf("IServ WebDAV konnte %s nicht öffnen (HTTP %d). Bitte erneut versuchen; bleibt der Fehler bestehen, wähle einen anderen Ordner", where, status)
	}
}

func webDAVClient(target string) (*http.Client, error) {
	targetURL, err := url.Parse(target)
	if err != nil || targetURL.Scheme != "https" || targetURL.Host == "" {
		return nil, fmt.Errorf("ungültiges WebDAV-Ziel")
	}
	return &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("zu viele WebDAV-Weiterleitungen")
			}
			if !strings.EqualFold(req.URL.Scheme, "https") || !strings.EqualFold(req.URL.Host, targetURL.Host) {
				return fmt.Errorf("unsichere WebDAV-Weiterleitung blockiert")
			}
			if len(via) > 0 && via[len(via)-1].Response != nil {
				status := via[len(via)-1].Response.StatusCode
				if status != http.StatusTemporaryRedirect && status != http.StatusPermanentRedirect {
					return fmt.Errorf("WebDAV-Weiterleitung würde die PROPFIND-Anfrage verändern")
				}
			}
			return nil
		},
	}, nil
}

func listWebDAV(baseURL, user, password, remotePath string) ([]string, error) {
	target, err := webdavURL(baseURL, remotePath)
	if err != nil {
		return nil, err
	}
	body := `<?xml version="1.0" encoding="utf-8" ?><d:propfind xmlns:d="DAV:"><d:prop><d:resourcetype/></d:prop></d:propfind>`
	req, err := http.NewRequest("PROPFIND", target, strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(user, password)
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	client, err := webDAVClient(target)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("IServ WebDAV ist nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 16*1024))
		return nil, webDAVStatusError(resp.StatusCode, string(data), remotePath)
	}
	const maxWebDAVResponse = 8 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxWebDAVResponse+1))
	if err != nil {
		return nil, fmt.Errorf("WebDAV-Antwort konnte nicht gelesen werden: %w", err)
	}
	if len(data) > maxWebDAVResponse {
		return nil, fmt.Errorf("WebDAV-Antwort ist unerwartet groß")
	}
	return decodeWebDAVDirectories(target, remotePath, strings.NewReader(string(data)))
}

func webDAVErrorForLang(err error, lang string) string {
	if err == nil {
		return ""
	}
	if lang != "en" {
		return err.Error()
	}
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "https verwenden"):
		return "The WebDAV address must use HTTPS."
	case strings.Contains(s, "ungültige webdav"):
		return "The WebDAV address or folder path is invalid."
	case strings.Contains(s, "http 401"):
		return "IServ rejected the sign-in (HTTP 401). Check your username and password or app password."
	case strings.Contains(s, "nicht zugänglich"):
		return "This folder is listed by IServ but cannot be opened through WebDAV. Choose another folder or ask your school administrator about WebDAV permissions."
	case strings.Contains(s, "nicht erreichbar"):
		return "IServ WebDAV could not be reached. Check the address and your internet connection."
	case strings.Contains(s, "unerwartet groß"):
		return "IServ returned an unexpectedly large WebDAV response."
	default:
		return "IServ WebDAV could not open this folder. Please try again or choose another folder."
	}
}
