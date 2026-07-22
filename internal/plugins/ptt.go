package plugins

import (
	"arcaeabot/internal/utils"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"arcaeabot/internal/napcat"
	"arcaeabot/internal/onebot"
)

type PTTPlugin struct {
	utils.Base
	ctx utils.Context
}

func (p *PTTPlugin) trend(ctx context.Context, ev napcat.Event, _ string) error {
	records, err := p.query(ctx, onebot.UserID(ev))
	if err != nil {
		return onebot.Reply(ctx, p.ctx.Client, ev, "PTT趋势图生成失败惹！")
	}
	if len(records) == 0 {
		return onebot.Reply(ctx, p.ctx.Client, ev, "未找到您的PTT数据")
	}
	out := filepath.Join(p.ctx.Config.TmpPath, "ptt_trend", "ptt_trend.png")
	if err := renderPTTTrend(out, p.ctx.Config.ResourcesPath, records); err != nil {
		return err
	}
	return utils.ReplyImg(ctx, p.ctx.Client, ev, out)
}

func (p *PTTPlugin) query(ctx context.Context, qq int64) ([]pttTrendRecord, error) {
	var userID, currentPTT, joinDate int64
	userID, err := utils.UserID(ctx, p.ctx.Arcaea, qq)
	if err != nil {
		return nil, err
	}
	if err := p.ctx.Arcaea.QueryRowContext(ctx, "SELECT rating_ptt, join_date FROM user WHERE user_id=?", userID).Scan(&currentPTT, &joinDate); err != nil {
		return nil, err
	}

	if p.ctx.ArcaeaLog == nil {
		return nil, fmt.Errorf("arcaea log database is not configured")
	}

	rows, err := p.ctx.ArcaeaLog.QueryContext(ctx, "SELECT time, rating_ptt FROM user_rating WHERE user_id=? ORDER BY time ASC", userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pttTrendRecord
	for rows.Next() {
		var rec pttTrendRecord
		if err := rows.Scan(&rec.Time, &rec.PTT); err != nil {
			return nil, err
		}
		rec.Time = unixSeconds(rec.Time)
		if rec.PTT > 100 {
			rec.PTT = rec.PTT / 100
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) > 0 {
		return out, nil
	}

	if currentPTT <= 0 {
		return nil, nil
	}
	joinDate = unixSeconds(joinDate)
	if joinDate <= 0 {
		joinDate = time.Now().Unix()
	}
	return []pttTrendRecord{{Time: joinDate, PTT: float64(currentPTT) / 100}}, nil
}

func unixSeconds(timestamp int64) int64 {
	for timestamp > 10_000_000_000 {
		timestamp /= 1000
	}
	return timestamp
}

type pttTrendRecord struct {
	Time int64
	PTT  float64
}

func renderPTTTrend(path, resourcesPath string, records []pttTrendRecord) error {
	const width = 950
	const height = 500
	const margin = 60
	const headerHeight = 80
	graphWidth := float64(width - 2*margin)
	graphHeight := float64(height - 2*margin - 20)

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	utils.RenderFillRect(img, 0, 0, width, height, color.White)
	titleFace := utils.LoadRenderFontPath(filepath.Join(resourcesPath, "fonts", "GeosansLight.ttf"), 36)
	labelFace := utils.LoadRenderFontPath(filepath.Join(resourcesPath, "fonts", "GeosansLight.ttf"), 24)
	if len(records) == 0 {
		titleFace = utils.LoadRenderFontPath(filepath.Join(resourcesPath, "fonts", "GeosansLight.ttf"), 44)
		utils.RenderDrawTextTop(img, titleFace, 50, height/2, "未找到PTT数据", color.Black)
		return writeTrendPNG(path, img)
	}

	for y := 0; y < 10; y++ {
		r := uint8(30 + 100*y*8/headerHeight)
		g := uint8(60 + 40*y*8/headerHeight)
		b := uint8(100 + 100*y*8/headerHeight)
		drawThickLine(img, 0, y, width, y, color.RGBA{r, g, b, 255}, 1)
	}

	minTime, maxTime := records[0].Time, records[0].Time
	maxPTT := records[0].PTT
	for _, rec := range records {
		if rec.Time < minTime {
			minTime = rec.Time
		}
		if rec.Time > maxTime {
			maxTime = rec.Time
		}
		if rec.PTT > maxPTT {
			maxPTT = rec.PTT
		}
	}
	minPTT := 0.0
	if maxPTT <= 0 {
		maxPTT = 10.0
	}

	for i := 0; i < int(graphWidth); i += 2 {
		x := margin + i
		r := uint8(100 + 100*i/int(graphWidth))
		g := uint8(150 + 50*i/int(graphWidth))
		b := uint8(200 - 50*i/int(graphWidth))
		drawThickLine(img, x, height-margin, x+1, height-margin, color.RGBA{r, g, b, 255}, 2)
	}
	for i := 0; i < int(graphHeight); i += 2 {
		y := height - margin - i
		r := uint8(100 + 100*i/int(graphHeight))
		g := uint8(150 + 50*i/int(graphHeight))
		b := uint8(200 - 50*i/int(graphHeight))
		drawThickLine(img, margin, y, margin, y-1, color.RGBA{r, g, b, 255}, 2)
	}

	points := make([][2]float64, 0, len(records))
	timeRange := float64(maxTime - minTime)
	if timeRange == 0 {
		timeRange = 1
	}
	pttRange := maxPTT - minPTT
	if pttRange == 0 {
		pttRange = 1
	}
	for _, rec := range records {
		x := float64(margin) + (float64(rec.Time-minTime)/timeRange)*graphWidth
		y := float64(height-margin) - ((rec.PTT-minPTT)/pttRange)*graphHeight
		points = append(points, [2]float64{x, y})
	}
	for i := 0; i < len(points)-1; i++ {
		r := uint8(50 + 150*i/len(points))
		g := uint8(100 + 100*i/len(points))
		b := uint8(200 - 100*i/len(points))
		drawThickLine(img, int(points[i][0]), int(points[i][1]), int(points[i+1][0]), int(points[i+1][1]), color.RGBA{r, g, b, 255}, 3)
	}
	for _, pt := range points {
		utils.RenderDrawEllipse(img, int(pt[0]-4), int(pt[1]-4), int(pt[0]+4), int(pt[1]+4), color.RGBA{255, 0, 0, 255})
		drawEllipseOutline(img, int(pt[0]-4), int(pt[1]-4), int(pt[0]+4), int(pt[1]+4), color.Black)
	}

	title := "PTT Trend"
	utils.RenderDrawTextTop(img, titleFace, (width-utils.RenderTextWidth(titleFace, title))/2, 20, title, color.Black)
	yStep := maxPTT / 5
	for i := 0; i < 6; i++ {
		yPTT := float64(i) * yStep
		yPos := float64(height-margin) - ((yPTT-minPTT)/pttRange)*graphHeight
		utils.RenderDrawTextTop(img, labelFace, margin-58, int(yPos)-10, formatFixedWidthPTT(yPTT), color.Black)
	}
	for i := 0; i < 11; i++ {
		xTime := float64(minTime) + float64(maxTime-minTime)*float64(i)/10
		xPos := float64(margin) + float64(i)/10*graphWidth
		date := time.Unix(int64(xTime), 0).Format("01/02")
		utils.RenderDrawTextTop(img, labelFace, int(xPos)-20, height-margin+10, date, color.Black)
	}
	return writeTrendPNG(path, img)
}

func writeTrendPNG(path string, img *image.RGBA) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func drawThickLine(img *image.RGBA, x1, y1, x2, y2 int, c color.Color, width int) {
	if width <= 1 {
		utils.RenderDrawLine(img, x1, y1, x2, y2, c)
		return
	}
	r := width / 2
	for dx := -r; dx <= r; dx++ {
		for dy := -r; dy <= r; dy++ {
			utils.RenderDrawLine(img, x1+dx, y1+dy, x2+dx, y2+dy, c)
		}
	}
}

func drawEllipseOutline(img *image.RGBA, x1, y1, x2, y2 int, c color.Color) {
	cx := float64(x1+x2) / 2
	cy := float64(y1+y2) / 2
	rx := float64(x2-x1) / 2
	ry := float64(y2-y1) / 2
	for deg := 0.0; deg < 360; deg += 2 {
		rad := deg * math.Pi / 180
		x := int(cx + math.Cos(rad)*rx)
		y := int(cy + math.Sin(rad)*ry)
		if image.Pt(x, y).In(img.Bounds()) {
			img.Set(x, y, c)
		}
	}
}

func formatFixedWidthPTT(v float64) string {
	s := strconvFormatFloat(v, 2)
	for len(s) < 5 {
		s = "0" + s
	}
	return s
}

func strconvFormatFloat(v float64, precision int) string {
	pow := math.Pow10(precision)
	rounded := math.Round(v*pow) / pow
	return strconv.FormatFloat(rounded, 'f', precision, 64)
}

func init() {
	utils.Add("ptt", func(ctx utils.Context) *PTTPlugin {
		p := &PTTPlugin{ctx: ctx}
		p.Init("ptt")
		p.Command("#趋势", nil, p.trend, "生成PTT趋势图")
		return p
	}, "ptt_trend")
}
