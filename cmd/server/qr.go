package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strconv"
	"strings"
)

// ========== 极简二维码生成（字节模式 / 纠错等级 L / 版本 1-5 / 单纠错块） ==========
//
// 离线环境无法引入第三方二维码库，这里按规范实现一个够用的子集：
//   - 只支持字节模式（URL 足够）
//   - 纠错等级固定 L，版本 1-5（容量 17/32/53/78/106 字节，都是单纠错块，无需交织）
//   - 版本 1-6 不需要版本信息块，实现更不容易出错
// 输出 PNG，供桌面端用 Image.network 直接显示，手机相机可扫。

type qrVer struct {
	version  int
	size     int
	dataW    int // 数据码字数
	ecW      int // 纠错码字数
	alignPos int // 对齐图案坐标（v1 无，用 -1）
}

var qrVersions = []qrVer{
	{1, 21, 19, 7, -1},
	{2, 25, 34, 10, 18},
	{3, 29, 55, 15, 22},
	{4, 33, 80, 20, 26},
	{5, 37, 108, 26, 30},
}

// ---------- GF(256) ----------

var qrGFExp [512]byte
var qrGFLog [256]byte

func init() {
	x := 1
	for i := 0; i < 255; i++ {
		qrGFExp[i] = byte(x)
		qrGFLog[x] = byte(i)
		x <<= 1
		if x&0x100 != 0 {
			x ^= 0x11D
		}
	}
	for i := 255; i < 512; i++ {
		qrGFExp[i] = qrGFExp[i-255]
	}
}

func qrMul(a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return qrGFExp[int(qrGFLog[a])+int(qrGFLog[b])]
}

func qrPolyMul(a, b []byte) []byte {
	res := make([]byte, len(a)+len(b)-1)
	for i := range a {
		for j := range b {
			res[i+j] ^= qrMul(a[i], b[j])
		}
	}
	return res
}

// qrRSEncode 计算 ecLen 个纠错码字
func qrRSEncode(data []byte, ecLen int) []byte {
	gen := []byte{1}
	for i := 0; i < ecLen; i++ {
		gen = qrPolyMul(gen, []byte{1, qrGFExp[i]})
	}
	res := make([]byte, ecLen)
	for _, d := range data {
		factor := d ^ res[0]
		copy(res, res[1:])
		res[ecLen-1] = 0
		if factor != 0 {
			for i := 0; i < ecLen; i++ {
				res[i] ^= qrMul(gen[i+1], factor)
			}
		}
	}
	return res
}

// ---------- 比特流 ----------

func qrBuildCodewords(v qrVer, text string) []byte {
	var bits []bool
	appendBits := func(val int, n int) {
		for i := n - 1; i >= 0; i-- {
			bits = append(bits, (val>>uint(i))&1 == 1)
		}
	}

	appendBits(0b0100, 4) // 字节模式
	appendBits(len(text), 8)
	for i := 0; i < len(text); i++ {
		appendBits(int(text[i]), 8)
	}

	totalBits := v.dataW * 8
	for i := 0; i < 4 && len(bits) < totalBits; i++ {
		bits = append(bits, false)
	}
	for len(bits)%8 != 0 {
		bits = append(bits, false)
	}
	pad := []byte{0xEC, 0x11}
	pi := 0
	for len(bits) < totalBits {
		appendBits(int(pad[pi%2]), 8)
		pi++
	}

	out := make([]byte, len(bits)/8)
	for i, b := range bits {
		if b {
			out[i/8] |= 1 << uint(7-(i%8))
		}
	}
	return out
}

// ---------- 矩阵 ----------

type qrMatrix struct {
	size int
	mod  [][]bool
	fn   [][]bool
}

func newQRMatrix(size int) *qrMatrix {
	m := &qrMatrix{size: size}
	m.mod = make([][]bool, size)
	m.fn = make([][]bool, size)
	for i := 0; i < size; i++ {
		m.mod[i] = make([]bool, size)
		m.fn[i] = make([]bool, size)
	}
	return m
}

func (m *qrMatrix) setFn(row, col int, dark bool) {
	if row < 0 || row >= m.size || col < 0 || col >= m.size {
		return
	}
	m.mod[row][col] = dark
	m.fn[row][col] = true
}

func (m *qrMatrix) drawFunctionPatterns(v qrVer) {
	size := m.size

	// 三个定位图案 + 分隔符
	drawFinder := func(row, col int) {
		for dr := -1; dr <= 7; dr++ {
			for dc := -1; dc <= 7; dc++ {
				r, c := row+dr, col+dc
				if r < 0 || r >= size || c < 0 || c >= size {
					continue
				}
				dark := false
				if dr >= 0 && dr <= 6 && dc >= 0 && dc <= 6 {
					if dr == 0 || dr == 6 || dc == 0 || dc == 6 {
						dark = true
					} else if dr >= 2 && dr <= 4 && dc >= 2 && dc <= 4 {
						dark = true
					}
				}
				m.setFn(r, c, dark)
			}
		}
	}
	drawFinder(0, 0)
	drawFinder(0, size-7)
	drawFinder(size-7, 0)

	// 定时图案
	for i := 8; i < size-8; i++ {
		m.setFn(6, i, i%2 == 0)
		m.setFn(i, 6, i%2 == 0)
	}

	// 对齐图案（v2-v5 各一个）
	if v.alignPos > 0 {
		for dr := -2; dr <= 2; dr++ {
			for dc := -2; dc <= 2; dc++ {
				dark := dr == -2 || dr == 2 || dc == -2 || dc == 2 || (dr == 0 && dc == 0)
				m.setFn(v.alignPos+dr, v.alignPos+dc, dark)
			}
		}
	}

	// 固定黑模块 + 预留格式信息区
	m.setFn(size-8, 8, true)
	for i := 0; i <= 8; i++ {
		m.setFn(8, i, m.mod[8][i])
		m.setFn(i, 8, m.mod[i][8])
	}
	for i := size - 8; i < size; i++ {
		m.setFn(8, i, m.mod[8][i])
		m.setFn(i, 8, m.mod[i][8])
	}
}

// drawFormatBits 写入两份格式信息（纠错等级 L）
func (m *qrMatrix) drawFormatBits(mask int) {
	size := m.size
	data := (0b01 << 3) | mask // 纠错等级 L = 01
	rem := data
	for i := 0; i < 10; i++ {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	bits := (data<<10 | rem) ^ 0x5412

	getBit := func(i int) bool { return (bits>>uint(i))&1 == 1 }

	// 第一份
	for i := 0; i <= 5; i++ {
		m.setFn(i, 8, getBit(i))
	}
	m.setFn(7, 8, getBit(6))
	m.setFn(8, 8, getBit(7))
	m.setFn(8, 7, getBit(8))
	for i := 9; i < 15; i++ {
		m.setFn(8, 14-i, getBit(i))
	}

	// 第二份
	for i := 0; i < 8; i++ {
		m.setFn(8, size-1-i, getBit(i))
	}
	for i := 8; i < 15; i++ {
		m.setFn(size-15+i, 8, getBit(i))
	}
	m.setFn(size-8, 8, true)
}

func (m *qrMatrix) drawCodewords(data []byte) {
	size := m.size
	i := 0
	total := len(data) * 8
	for right := size - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := 0; vert < size; vert++ {
			for j := 0; j < 2; j++ {
				col := right - j
				upward := ((right + 1) & 2) == 0
				row := vert
				if upward {
					row = size - 1 - vert
				}
				if !m.fn[row][col] && i < total {
					m.mod[row][col] = (data[i>>3]>>uint(7-(i&7)))&1 == 1
					i++
				}
			}
		}
	}
}

func qrMaskBit(mask, row, col int) bool {
	switch mask {
	case 0:
		return (row+col)%2 == 0
	case 1:
		return row%2 == 0
	case 2:
		return col%3 == 0
	case 3:
		return (row+col)%3 == 0
	case 4:
		return (row/2+col/3)%2 == 0
	case 5:
		return (row*col)%2+(row*col)%3 == 0
	case 6:
		return ((row*col)%2+(row*col)%3)%2 == 0
	case 7:
		return ((row+col)%2+(row*col)%3)%2 == 0
	}
	return false
}

func (m *qrMatrix) applyMask(mask int) {
	for row := 0; row < m.size; row++ {
		for col := 0; col < m.size; col++ {
			if m.fn[row][col] {
				continue
			}
			if qrMaskBit(mask, row, col) {
				m.mod[row][col] = !m.mod[row][col]
			}
		}
	}
}

// penalty 粗略评分（只用于挑掩码，选错也不影响可解码性）
func (m *qrMatrix) penalty() int {
	size := m.size
	score := 0
	// 规则 1：连续同色
	for row := 0; row < size; row++ {
		run := 1
		for col := 1; col < size; col++ {
			if m.mod[row][col] == m.mod[row][col-1] {
				run++
			} else {
				if run >= 5 {
					score += 3 + (run - 5)
				}
				run = 1
			}
		}
		if run >= 5 {
			score += 3 + (run - 5)
		}
	}
	for col := 0; col < size; col++ {
		run := 1
		for row := 1; row < size; row++ {
			if m.mod[row][col] == m.mod[row-1][col] {
				run++
			} else {
				if run >= 5 {
					score += 3 + (run - 5)
				}
				run = 1
			}
		}
		if run >= 5 {
			score += 3 + (run - 5)
		}
	}
	// 规则 2：2x2 同色块
	for row := 0; row < size-1; row++ {
		for col := 0; col < size-1; col++ {
			c := m.mod[row][col]
			if c == m.mod[row][col+1] && c == m.mod[row+1][col] && c == m.mod[row+1][col+1] {
				score += 3
			}
		}
	}
	// 规则 3：类似定位图案的 1:1:3:1:1
	pattern1 := []bool{true, false, true, true, true, false, true, false, false, false, false}
	pattern2 := []bool{false, false, false, false, true, false, true, true, true, false, true}
	match := func(vals []bool, i int, p []bool) bool {
		for k := 0; k < len(p); k++ {
			if vals[i+k] != p[k] {
				return false
			}
		}
		return true
	}
	for row := 0; row < size; row++ {
		vals := make([]bool, size)
		for col := 0; col < size; col++ {
			vals[col] = m.mod[row][col]
		}
		for i := 0; i+len(pattern1) <= size; i++ {
			if match(vals, i, pattern1) || match(vals, i, pattern2) {
				score += 40
			}
		}
	}
	for col := 0; col < size; col++ {
		vals := make([]bool, size)
		for row := 0; row < size; row++ {
			vals[row] = m.mod[row][col]
		}
		for i := 0; i+len(pattern1) <= size; i++ {
			if match(vals, i, pattern1) || match(vals, i, pattern2) {
				score += 40
			}
		}
	}
	return score
}

// qrEncode 生成二维码矩阵（纠错等级 L）
func qrEncode(text string) (*qrMatrix, error) {
	if len(text) == 0 {
		return nil, fmt.Errorf("empty text")
	}
	var chosen *qrVer
	for i := range qrVersions {
		v := &qrVersions[i]
		if len(text) <= v.dataW-2 {
			chosen = v
			break
		}
	}
	if chosen == nil {
		return nil, fmt.Errorf("text too long (max %d bytes)", qrVersions[len(qrVersions)-1].dataW-2)
	}

	dataCW := qrBuildCodewords(*chosen, text)
	ecCW := qrRSEncode(dataCW, chosen.ecW)
	all := append(append([]byte{}, dataCW...), ecCW...)

	best := (*qrMatrix)(nil)
	bestScore := 1 << 30
	for mask := 0; mask < 8; mask++ {
		m := newQRMatrix(chosen.size)
		m.drawFunctionPatterns(*chosen)
		m.drawCodewords(all)
		m.applyMask(mask)
		m.drawFormatBits(mask)
		s := m.penalty()
		if s < bestScore {
			bestScore = s
			best = m
		}
	}
	return best, nil
}

// ---------- PNG 输出 ----------

func qrPNG(text string, scale, quiet int) ([]byte, error) {
	m, err := qrEncode(text)
	if err != nil {
		return nil, err
	}
	if scale < 2 {
		scale = 2
	}
	if quiet < 1 {
		quiet = 1
	}
	dim := (m.size + quiet*2) * scale
	img := image.NewRGBA(image.Rect(0, 0, dim, dim))
	white := color.RGBA{255, 255, 255, 255}
	black := color.RGBA{0, 0, 0, 255}
	for y := 0; y < dim; y++ {
		for x := 0; x < dim; x++ {
			img.Set(x, y, white)
		}
	}
	for row := 0; row < m.size; row++ {
		for col := 0; col < m.size; col++ {
			if !m.mod[row][col] {
				continue
			}
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					img.Set((col+quiet)*scale+dx, (row+quiet)*scale+dy, black)
				}
			}
		}
	}
	out := newByteBuffer()
	if err := png.Encode(out, img); err != nil {
		return nil, err
	}
	return out.bytes(), nil
}

// ---------- 简易字节缓冲 ----------

type byteBuffer struct{ data []byte }

func newByteBuffer() *byteBuffer { return &byteBuffer{} }
func (b *byteBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}
func (b *byteBuffer) bytes() []byte { return b.data }

// handleQRCode 生成二维码 PNG：GET /api/v1/qr?text=<url>
func handleQRCode(w http.ResponseWriter, r *http.Request) {
	text := strings.TrimSpace(r.URL.Query().Get("text"))
	if text == "" {
		writeJSON(w, 400, map[string]string{"error": "missing text"})
		return
	}
	if len(text) > 100 {
		writeJSON(w, 400, map[string]string{"error": "text too long (max 100 bytes)"})
		return
	}
	scale := 8
	if s := r.URL.Query().Get("scale"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n >= 2 && n <= 20 {
			scale = n
		}
	}
	pngData, err := qrPNG(text, scale, 4)
	if err != nil {
		writeJSON(w, 400, map[string]string{"error": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(pngData)
}