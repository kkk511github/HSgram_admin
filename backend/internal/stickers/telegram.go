package stickers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type BotAPIClient struct {
	token  string
	http   *http.Client
	apiURL string
}

func NewBotAPIClient(token string, httpClient *http.Client) *BotAPIClient {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &BotAPIClient{
		token:  token,
		http:   httpClient,
		apiURL: "https://api.telegram.org",
	}
}

func (c *BotAPIClient) GetStickerSet(ctx context.Context, shortName string) (TelegramStickerSet, error) {
	var response struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      struct {
			Name        string `json:"name"`
			Title       string `json:"title"`
			StickerType string `json:"sticker_type"`
			Stickers    []struct {
				FileID       string   `json:"file_id"`
				FileUniqueID string   `json:"file_unique_id"`
				Type         string   `json:"type"`
				Emoji        string   `json:"emoji"`
				Width        int32    `json:"width"`
				Height       int32    `json:"height"`
				FileSize     int64    `json:"file_size"`
				Keywords     []string `json:"keywords"`
			} `json:"stickers"`
		} `json:"result"`
	}
	if err := c.getJSON(ctx, "/bot"+c.token+"/getStickerSet?name="+url.QueryEscape(shortName), &response); err != nil {
		return TelegramStickerSet{}, err
	}
	if !response.OK {
		return TelegramStickerSet{}, fmt.Errorf("telegram getStickerSet failed: %s", response.Description)
	}
	out := TelegramStickerSet{
		Name:        response.Result.Name,
		Title:       response.Result.Title,
		StickerType: response.Result.StickerType,
		Stickers:    make([]TelegramSticker, 0, len(response.Result.Stickers)),
	}
	for _, item := range response.Result.Stickers {
		out.Stickers = append(out.Stickers, TelegramSticker{
			FileID:       item.FileID,
			FileUniqueID: item.FileUniqueID,
			Type:         item.Type,
			Emoji:        item.Emoji,
			Width:        item.Width,
			Height:       item.Height,
			FileSize:     item.FileSize,
			Keywords:     item.Keywords,
		})
	}
	return out, nil
}

func (c *BotAPIClient) GetFile(ctx context.Context, fileID string) (TelegramFile, error) {
	var response struct {
		OK          bool   `json:"ok"`
		Description string `json:"description"`
		Result      struct {
			FileID   string `json:"file_id"`
			FilePath string `json:"file_path"`
			FileSize int64  `json:"file_size"`
		} `json:"result"`
	}
	if err := c.getJSON(ctx, "/bot"+c.token+"/getFile?file_id="+url.QueryEscape(fileID), &response); err != nil {
		return TelegramFile{}, err
	}
	if !response.OK {
		return TelegramFile{}, fmt.Errorf("telegram getFile failed: %s", response.Description)
	}
	return TelegramFile{FileID: response.Result.FileID, FilePath: response.Result.FilePath, FileSize: response.Result.FileSize}, nil
}

func (c *BotAPIClient) Download(ctx context.Context, filePath string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL+"/file/bot"+c.token+"/"+strings.TrimLeft(filePath, "/"), nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("telegram file download status %d", res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, MaxStickerFileSize+1))
}

func (c *BotAPIClient) getJSON(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL+path, nil)
	if err != nil {
		return err
	}
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("telegram api status %d", res.StatusCode)
	}
	return json.NewDecoder(res.Body).Decode(out)
}
