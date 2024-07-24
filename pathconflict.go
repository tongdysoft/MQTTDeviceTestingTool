package main

// 本文件集中判断输入材料与输出目标是否指向同一文件。
// 目标是在任何截断、写入、创建或替换之前失败，保全已有的证书、私钥、
// 私钥密码文件、认证配置以及上一轮结果。
//
// 说明：这里做的是文件系统身份检查，不能消除检查与真正打开之间仍然存在的
// 并发替换窗口；需要独立打开文件的入口（OpenEventLog、NewDiagnostics）
// 会在打开后再次比较文件身份，并在截断前放弃。

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// PathRoleKind 区分只读输入与可写输出。
type PathRoleKind int

const (
	// PathRoleInput 表示启动时读取的材料。
	PathRoleInput PathRoleKind = iota
	// PathRoleOutput 表示会被创建、截断或替换的目标。
	PathRoleOutput
)

// FileRole 描述一个路径在运行时的用途。
// Flag 是产生该路径的命令行参数，Role 是面向用户的角色说明；
// 两者只用于构造冲突错误，不包含文件内容。
type FileRole struct {
	Flag string
	Role string
	Path string
	Kind PathRoleKind
	// IsDir 表示该角色必须指向目录（目前仅 -results）。
	IsDir bool
}

// InputFileRoles 返回启动时只读的输入材料。
func (c *Config) InputFileRoles() []FileRole {
	return []FileRole{
		{Flag: "-server-cert", Role: "server certificate", Path: c.ServerCertFile, Kind: PathRoleInput},
		{Flag: "-server-key", Role: "server private key", Path: c.ServerKeyFile, Kind: PathRoleInput},
		{Flag: "-server-key-password-file", Role: "server key password file", Path: c.ServerKeyPasswordFile, Kind: PathRoleInput},
		{Flag: "-client-ca", Role: "client CA bundle", Path: c.ClientCAFile, Kind: PathRoleInput},
		{Flag: "-users", Role: "user authentication file", Path: c.UserFile, Kind: PathRoleInput},
	}
}

// LogFileRoles 返回三个 CSV/文本日志输出。
func (c *Config) LogFileRoles() []FileRole {
	return []FileRole{
		{Flag: "-log", Role: "text log", Path: c.LogFile, Kind: PathRoleOutput},
		{Flag: "-log-status", Role: "status CSV log", Path: c.LogStatusFile, Kind: PathRoleOutput},
		{Flag: "-log-data", Role: "data CSV log", Path: c.LogDataFile, Kind: PathRoleOutput},
	}
}

// FileRoles 返回全部参与冲突检查的输入与输出。
func (c *Config) FileRoles() []FileRole {
	roles := c.InputFileRoles()
	roles = append(roles, c.LogFileRoles()...)
	roles = append(roles, resultsFileRoles(c.ResultsDir)...)
	return roles
}

// resultsFileRoles 返回 -results 下实际使用的目录与两个文件。
func resultsFileRoles(dir string) []FileRole {
	if strings.TrimSpace(dir) == "" {
		return nil
	}
	return []FileRole{
		{Flag: "-results", Role: "results directory", Path: dir, Kind: PathRoleOutput, IsDir: true},
		{Flag: "-results", Role: "diagnostics events file (events.jsonl)", Path: filepath.Join(dir, eventsFileName), Kind: PathRoleOutput},
		{Flag: "-results", Role: "run summary file (summary.json)", Path: filepath.Join(dir, summaryFileName), Kind: PathRoleOutput},
	}
}

// CheckFileRoleConflicts 检查输入与输出、输出与输出是否指向同一文件。
// 输入之间允许复用（例如同一个证书包用于多个只读用途）。
// 所有错误都只说明参数、角色和路径，不读取文件内容。
func CheckFileRoleConflicts(roles []FileRole) error {
	type resolvedRole struct {
		role     FileRole
		identity pathIdentity
	}
	resolved := make([]resolvedRole, 0, len(roles))
	for _, role := range roles {
		if strings.TrimSpace(role.Path) == "" {
			continue
		}
		identity, err := resolvePathIdentity(role.Path)
		if err != nil {
			return fmt.Errorf("%s: %w", roleLabel(role), err)
		}
		// 同一角色重复出现（例如 Config 与显式 ResultsDir 都描述同一结果目录）
		// 不算输出冲突；角色不同但路径相同仍会被下面的成对检查拒绝。
		duplicate := false
		for _, existing := range resolved {
			if existing.role.Kind == role.Kind && existing.role.Flag == role.Flag &&
				existing.role.Role == role.Role && existing.role.IsDir == role.IsDir &&
				existing.identity.sameAs(identity) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		resolved = append(resolved, resolvedRole{role: role, identity: identity})
	}

	for i := 0; i < len(resolved); i++ {
		for j := i + 1; j < len(resolved); j++ {
			first, second := resolved[i], resolved[j]
			if first.role.Kind == PathRoleInput && second.role.Kind == PathRoleInput {
				continue
			}
			if !first.identity.sameAs(second.identity) {
				continue
			}
			return conflictError(first.role, second.role, second.identity.canonical)
		}
	}

	// 冲突优先于类型检查，这样 -log 指向 -results 目录时会同时指出两个参数。
	for _, item := range resolved {
		if !item.identity.exists {
			continue
		}
		if item.role.IsDir && !item.identity.info.IsDir() {
			return fmt.Errorf("%s is not a directory: %s", roleLabel(item.role), displayPath(item.identity.canonical))
		}
		if !item.role.IsDir && item.role.Kind == PathRoleOutput && item.identity.info.IsDir() {
			return fmt.Errorf("%s is a directory, not a file: %s", roleLabel(item.role), displayPath(item.identity.canonical))
		}
	}
	return nil
}

func roleLabel(role FileRole) string {
	if role.Flag == "" {
		return role.Role
	}
	return fmt.Sprintf("%s (%s)", role.Flag, role.Role)
}

func conflictError(first, second FileRole, path string) error {
	return fmt.Errorf("file path conflict: %s and %s refer to the same file: %s",
		roleLabel(first), roleLabel(second), displayPath(path))
}

// displayPath 只用于错误信息，不参与身份比较。
func displayPath(path string) string {
	if path == "" {
		return path
	}
	return filepath.Clean(path)
}

// pathIdentity 保存用于比较的路径身份。
// 已存在的文件使用 os.FileInfo 比较（可识别符号链接与硬链接）；
// 不存在的输出使用规范化后的父目录路径比较。
type pathIdentity struct {
	canonical string
	exists    bool
	info      os.FileInfo
	// foldCase 仅对不存在的路径有意义，取值为其父目录所在文件系统的
	// 真实大小写语义。
	foldCase bool
}

func (identity pathIdentity) sameAs(other pathIdentity) bool {
	switch {
	case identity.exists && other.exists:
		return os.SameFile(identity.info, other.info)
	case !identity.exists && !other.exists:
		if identity.canonical == other.canonical {
			return true
		}
		return identity.foldCase && other.foldCase && strings.EqualFold(identity.canonical, other.canonical)
	default:
		// 两个路径如果真正指向同一文件，os.Stat 会同时成功；
		// 一个存在、一个不存在时不视为同一目标。
		return false
	}
}

// resolvePathIdentity 解析路径身份。相对路径基于当前工作目录解析，
// 不先做词法 Clean，以免破坏 "符号链接/.." 的实际语义。
func resolvePathIdentity(path string) (pathIdentity, error) {
	absolute, err := absolutePath(path)
	if err != nil {
		return pathIdentity{}, err
	}
	info, statErr := os.Stat(absolute)
	if statErr == nil {
		return pathIdentity{canonical: absolute, exists: true, info: info}, nil
	}
	if !os.IsNotExist(statErr) {
		return pathIdentity{}, fmt.Errorf("cannot determine the file identity of %s: %w", absolute, statErr)
	}
	if link, err := os.Lstat(absolute); err == nil && link.Mode()&os.ModeSymlink != 0 {
		return pathIdentity{}, fmt.Errorf("path %s is a symbolic link that cannot be resolved", absolute)
	} else if err != nil && !os.IsNotExist(err) {
		return pathIdentity{}, fmt.Errorf("cannot inspect path %s: %w", absolute, err)
	}
	return resolveMissingPath(absolute)
}

// resolveMissingPath 找到最深的已存在父目录，跟随其中的符号链接，
// 再把尚不存在的剩余部分规范化后拼接回去。
func resolveMissingPath(absolute string) (pathIdentity, error) {
	current := absolute
	var remainder []string
	for {
		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return pathIdentity{}, fmt.Errorf("cannot determine the identity of %s because %s is not a directory", absolute, current)
			}
			real, err := filepath.EvalSymlinks(current)
			if err != nil {
				return pathIdentity{}, fmt.Errorf("cannot resolve symbolic links in %s: %w", current, err)
			}
			parts := append([]string{real}, reverseCopy(remainder)...)
			return pathIdentity{
				canonical: filepath.Join(parts...),
				foldCase:  dirIsCaseInsensitive(real),
			}, nil
		}
		if !os.IsNotExist(err) {
			return pathIdentity{}, fmt.Errorf("cannot determine the file identity of %s: %w", absolute, err)
		}
		if link, err := os.Lstat(current); err == nil && link.Mode()&os.ModeSymlink != 0 {
			return pathIdentity{}, fmt.Errorf("path %s is a symbolic link that cannot be resolved", current)
		}
		parent, base, ok := splitLastComponent(current)
		if !ok {
			return pathIdentity{}, fmt.Errorf("cannot find an existing parent directory for %s", absolute)
		}
		if base == ".." {
			// 内核在遍历时要求每个中间分量都存在，缺失目录之后的 ".." 无法解析。
			return pathIdentity{}, fmt.Errorf("path %s contains %q after a component that does not exist", absolute, "..")
		}
		remainder = append(remainder, base)
		current = parent
	}
}

// splitLastComponent 仅删除最后一个路径分量，不对剩余部分做词法 Clean，
// 以免把 "符号链接/.." 提前折叠成 "符号链接的父目录"，破坏内核解析语义。
func splitLastComponent(path string) (parent, base string, ok bool) {
	end := len(path)
	for end > 1 && os.IsPathSeparator(path[end-1]) {
		end--
	}
	start := end
	for start > 0 && !os.IsPathSeparator(path[start-1]) {
		start--
	}
	base = path[start:end]
	if start == 0 {
		return "", base, false
	}
	parent = path[:start-1]
	if parent == "" {
		parent = string(os.PathSeparator)
	}
	// Windows 下 "C:" 与 "C:" 的父目录必须保留根分隔符。
	if runtime.GOOS == "windows" && len(parent) == 2 && parent[1] == ':' {
		parent += string(os.PathSeparator)
	}
	if parent == path {
		return "", base, false
	}
	return parent, base, true
}

// absolutePath 把相对路径接到当前工作目录后，保留 "." 与 ".." 交由操作系统解析。
func absolutePath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	if volume := filepath.VolumeName(path); volume != "" {
		return "", fmt.Errorf("cannot resolve drive-relative path %q; use an absolute path", path)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("cannot determine the current directory: %w", err)
	}
	return cwd + string(os.PathSeparator) + path, nil
}

func reverseCopy(items []string) []string {
	reversed := make([]string, len(items))
	for i, item := range items {
		reversed[len(items)-1-i] = item
	}
	return reversed
}

var caseInsensitiveCache sync.Map

// dirIsCaseInsensitive 探测目录所在文件系统的真实大小写语义。
// 先尝试用大小写翻转后的目录名做只读比较；无法探测时退回平台默认值。
func dirIsCaseInsensitive(dir string) bool {
	if cached, ok := caseInsensitiveCache.Load(dir); ok {
		return cached.(bool)
	}
	value := probeDirCaseInsensitive(dir)
	caseInsensitiveCache.Store(dir, value)
	return value
}

func probeDirCaseInsensitive(dir string) bool {
	base := filepath.Base(dir)
	flipped := swapASCII(base)
	if flipped == base {
		return platformCaseInsensitive()
	}
	self, err := os.Stat(dir)
	if err != nil {
		return platformCaseInsensitive()
	}
	other, err := os.Stat(filepath.Join(filepath.Dir(dir), flipped))
	if err != nil {
		return platformCaseInsensitive()
	}
	return os.SameFile(self, other)
}

func swapASCII(value string) string {
	runes := []rune(value)
	for i, r := range runes {
		switch {
		case r >= 'a' && r <= 'z':
			runes[i] = r - 'a' + 'A'
		case r >= 'A' && r <= 'Z':
			runes[i] = r - 'A' + 'a'
		}
	}
	return string(runes)
}

// platformCaseInsensitive 是无法探测时的保守默认值。
func platformCaseInsensitive() bool {
	return runtime.GOOS == "windows" || runtime.GOOS == "darwin"
}
