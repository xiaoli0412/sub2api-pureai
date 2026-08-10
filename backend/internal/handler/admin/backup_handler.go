package admin

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type BackupHandler struct {
	backupService *service.BackupService
	userService   *service.UserService
	imageStorage  *service.ImageStorageSettingService
}

func NewBackupHandler(backupService *service.BackupService, userService *service.UserService, imageStorage *service.ImageStorageSettingService) *BackupHandler {
	return &BackupHandler{
		backupService: backupService,
		userService:   userService,
		imageStorage:  imageStorage,
	}
}

// ─── S3 配置 ───

func (h *BackupHandler) GetS3Config(c *gin.Context) {
	cfg, err := h.backupService.GetS3Config(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *BackupHandler) UpdateS3Config(c *gin.Context) {
	var req service.BackupS3Config
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	cfg, err := h.backupService.UpdateS3Config(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *BackupHandler) TestS3Connection(c *gin.Context) {
	var req service.BackupS3Config
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	err := h.backupService.TestS3Connection(c.Request.Context(), req)
	if err != nil {
		response.Success(c, gin.H{"ok": false, "message": err.Error()})
		return
	}
	response.Success(c, gin.H{"ok": true, "message": "connection successful"})
}

// ─── 定时备份 ───

func (h *BackupHandler) GetSchedule(c *gin.Context) {
	cfg, err := h.backupService.GetSchedule(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *BackupHandler) UpdateSchedule(c *gin.Context) {
	var req service.BackupScheduleConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	cfg, err := h.backupService.UpdateSchedule(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

// ─── 备份操作 ───

type CreateBackupRequest struct {
	ExpireDays *int `json:"expire_days"` // nil=使用默认值14，0=永不过期
}

func (h *BackupHandler) CreateBackup(c *gin.Context) {
	var req CreateBackupRequest
	_ = c.ShouldBindJSON(&req) // 允许空 body

	expireDays := 14 // 默认14天过期
	if req.ExpireDays != nil {
		expireDays = *req.ExpireDays
	}

	record, err := h.backupService.StartBackup(c.Request.Context(), "manual", expireDays)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Accepted(c, record)
}

// CreateLocalBackup POST /api/v1/admin/backups/local
// 创建本地备份：pg_dump -> gzip -> 本地磁盘文件，无需配置 S3。
type CreateLocalBackupRequest struct {
	ExpireDays *int    `json:"expire_days"` // nil=14，0=永不过期
	LocalDir   *string `json:"local_dir"`   // nil=使用默认目录 data/backups
}

func (h *BackupHandler) CreateLocalBackup(c *gin.Context) {
	var req CreateLocalBackupRequest
	_ = c.ShouldBindJSON(&req) // 允许空 body

	expireDays := 14
	if req.ExpireDays != nil {
		expireDays = *req.ExpireDays
	}
	localDir := ""
	if req.LocalDir != nil {
		localDir = strings.TrimSpace(*req.LocalDir)
	}

	record, err := h.backupService.StartLocalBackup(c.Request.Context(), "manual", expireDays, localDir)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Accepted(c, record)
}

// DownloadLocalBackup GET /api/v1/admin/backups/:id/download
// 直接流式返回本地备份文件（Content-Disposition: attachment）。
// 仅 StorageType=local 的备份可下载；S3 备份请走 /download-url 预签名链接。
func (h *BackupHandler) DownloadLocalBackup(c *gin.Context) {
	backupID := c.Param("id")
	if backupID == "" {
		response.BadRequest(c, "backup ID is required")
		return
	}
	path, err := h.backupService.GetLocalBackupPath(c.Request.Context(), backupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	fileName := filepath.Base(path)
	c.Header("Content-Disposition", "attachment; filename=\""+fileName+"\"")
	c.Header("Content-Type", "application/gzip")
	http.ServeFile(c.Writer, c.Request, path)
}

// isUnderLocalBackupDir 校验路径是否位于本地备份根目录之下，防止路径穿越下载任意文件。
func isUnderLocalBackupDir(path string) bool {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	root, err := filepath.Abs(service.DefaultLocalBackupDir)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return false
	}
	return rel != "." && !strings.HasPrefix(rel, "..") && !strings.HasPrefix(rel, "/")
}

func (h *BackupHandler) ListBackups(c *gin.Context) {
	records, err := h.backupService.ListBackups(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if records == nil {
		records = []service.BackupRecord{}
	}
	response.Success(c, gin.H{"items": records})
}

func (h *BackupHandler) GetBackup(c *gin.Context) {
	backupID := c.Param("id")
	if backupID == "" {
		response.BadRequest(c, "backup ID is required")
		return
	}
	record, err := h.backupService.GetBackupRecord(c.Request.Context(), backupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, record)
}

func (h *BackupHandler) DeleteBackup(c *gin.Context) {
	backupID := c.Param("id")
	if backupID == "" {
		response.BadRequest(c, "backup ID is required")
		return
	}
	if err := h.backupService.DeleteBackup(c.Request.Context(), backupID); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"deleted": true})
}

func (h *BackupHandler) GetDownloadURL(c *gin.Context) {
	backupID := c.Param("id")
	if backupID == "" {
		response.BadRequest(c, "backup ID is required")
		return
	}
	download, err := h.backupService.GetBackupDownloadURL(c.Request.Context(), backupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, download)
}

// ─── 恢复操作（需要重新输入管理员密码） ───

type RestoreBackupRequest struct {
	Password string `json:"password" binding:"required"`
}

func (h *BackupHandler) RestoreBackup(c *gin.Context) {
	backupID := c.Param("id")
	if backupID == "" {
		response.BadRequest(c, "backup ID is required")
		return
	}

	var req RestoreBackupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "password is required for restore operation")
		return
	}

	// 从上下文获取当前管理员用户 ID
	sub, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		response.Unauthorized(c, "unauthorized")
		return
	}

	// 获取管理员用户并验证密码
	user, err := h.userService.GetByID(c.Request.Context(), sub.UserID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if !user.CheckPassword(req.Password) {
		response.BadRequest(c, "incorrect admin password")
		return
	}

	record, err := h.backupService.StartRestore(c.Request.Context(), backupID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Accepted(c, record)
}

// ─── 异步生图对象存储配置 ───
//
// 与备份共用一套 S3 客户端构造，因此放在同一个页面下：勾选"复用备份 S3"即可直接
// 借用备份已配置的端点与密钥，只用不同的前缀区分对象（备份走 backups/，图片走 images/）。

func (h *BackupHandler) GetImageStorageConfig(c *gin.Context) {
	ctx := c.Request.Context()
	cfg, err := h.imageStorage.Get(ctx)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{
		"config":            cfg,
		"secret_configured": h.imageStorage.SecretConfigured(ctx),
	})
}

func (h *BackupHandler) UpdateImageStorageConfig(c *gin.Context) {
	var req service.ImageStorageSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	cfg, err := h.imageStorage.Update(c.Request.Context(), req)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *BackupHandler) TestImageStorageConnection(c *gin.Context) {
	var req service.ImageStorageSettings
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if err := h.imageStorage.TestConnection(c.Request.Context(), req); err != nil {
		response.Success(c, gin.H{"ok": false, "message": err.Error()})
		return
	}
	response.Success(c, gin.H{"ok": true, "message": "connection successful"})
}
