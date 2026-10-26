package update

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// 发布资产命名约定（资产文件名用 ASCII，内容/包名可用中文）。
const (
	// AppAssetName 是 macOS App 更新包。
	AppAssetName = "kuibu-macos-arm64.zip"
	// ChecksumsAssetName 是 `shasum -a 256` 生成的校验文件。
	ChecksumsAssetName = "SHA256SUMS"
)

// ProgressFunc 是 PrepareWithProgress 的进度回调。
//
// stage 取值：download（下载校验文件/更新包）、verify（校验更新包）、
// extract（解压更新包）。message 是给用户看的中文说明。
type ProgressFunc func(stage, message string)

// Prepare 下载并校验更新包，解压后返回新 .app 的路径。
//
// 只下载与解压，不碰当前 App；任何一步失败都返回错误，由调用方决定是否中止更新。
func Prepare(ctx context.Context, client *Client, rel *Release, workDir string) (string, error) {
	return PrepareWithProgress(ctx, client, rel, workDir, nil)
}

// PrepareWithProgress 与 Prepare 相同，但在下载、校验、解压时回调 progress。
func PrepareWithProgress(ctx context.Context, client *Client, rel *Release, workDir string, progress ProgressFunc) (string, error) {
	if client == nil {
		return "", fmt.Errorf("更新客户端不能为空")
	}
	if rel == nil {
		return "", fmt.Errorf("Release 不能为空")
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", fmt.Errorf("创建工作目录失败: %w", err)
	}

	notify := func(stage, message string) {
		if progress != nil {
			progress(stage, message)
		}
	}

	sumsAsset, ok := FindAsset(rel, ChecksumsAssetName)
	if !ok {
		return "", fmt.Errorf("Release 中缺少校验文件 %s", ChecksumsAssetName)
	}
	zipAsset, ok := FindAsset(rel, AppAssetName)
	if !ok {
		return "", fmt.Errorf("Release 中缺少更新包 %s", AppAssetName)
	}

	notify("download", "正在下载校验文件…")
	sumsPath := filepath.Join(workDir, ChecksumsAssetName)
	if err := downloadToFile(ctx, client, sumsAsset, sumsPath); err != nil {
		return "", err
	}

	sums, err := parseChecksumsFile(sumsPath)
	if err != nil {
		return "", err
	}
	want, ok := sums[AppAssetName]
	if !ok {
		return "", fmt.Errorf("校验文件 %s 中缺少 %s 的哈希", ChecksumsAssetName, AppAssetName)
	}

	notify("download", "正在下载更新包…")
	zipPath := filepath.Join(workDir, AppAssetName)
	if err := downloadToFile(ctx, client, zipAsset, zipPath); err != nil {
		return "", err
	}

	notify("verify", "正在校验更新包…")
	if err := VerifyFile(zipPath, want); err != nil {
		return "", fmt.Errorf("更新包校验失败: %w", err)
	}

	notify("extract", "正在解压更新包…")
	extractDir := filepath.Join(workDir, "extracted")
	if err := os.RemoveAll(extractDir); err != nil {
		return "", fmt.Errorf("清理解压目录失败: %w", err)
	}
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return "", fmt.Errorf("创建解压目录失败: %w", err)
	}
	if err := ExtractZip(zipPath, extractDir); err != nil {
		return "", err
	}

	newApp, err := FindAppBundle(extractDir)
	if err != nil {
		return "", err
	}
	return newApp, nil
}

// downloadToFile 把资产下载到指定文件。
func downloadToFile(ctx context.Context, client *Client, asset Asset, path string) error {
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("创建文件 %s 失败: %w", path, err)
	}
	if err := client.Download(ctx, asset, f); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("写入文件 %s 失败: %w", path, err)
	}
	return nil
}

// parseChecksumsFile 读取并解析校验和文件。
func parseChecksumsFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开校验文件失败: %w", err)
	}
	defer f.Close()

	sums, err := ParseChecksums(f)
	if err != nil {
		return nil, fmt.Errorf("解析校验文件失败: %w", err)
	}
	return sums, nil
}
