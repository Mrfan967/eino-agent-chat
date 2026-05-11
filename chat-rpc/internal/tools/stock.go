package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// StockTool 美股实时行情+历史K线工具（新浪财经，国内可直接访问，无需API Key）
type StockTool struct {
	client *http.Client
}

func NewStockTool() *StockTool {
	return &StockTool{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *StockTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "stock_query",
		Desc: "查询美股行情数据。可查实时行情，也可查近N天的每日K线历史数据。支持美股代码如 AAPL(苹果)、GOOGL(谷歌)、MSFT(微软)、TSLA(特斯拉)、NVDA(英伟达)等",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"symbol": {
				Type:     schema.String,
				Desc:     "股票代码，如 AAPL, GOOGL, MSFT, TSLA, NVDA, AMZN, META",
				Required: true,
			},
			"days": {
				Type:     schema.Integer,
				Desc:     "查询近N天的历史K线数据，默认0表示只查实时行情，设为7则查近7天每日数据",
				Required: false,
			},
		}),
	}, nil
}

func (s *StockTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	var params map[string]interface{}
	if err := json.Unmarshal([]byte(argumentsInJSON), &params); err != nil {
		return "", fmt.Errorf("解析参数失败: %v", err)
	}

	symbol, ok := params["symbol"].(string)
	if !ok || symbol == "" {
		return "", fmt.Errorf("请提供股票代码，如 AAPL")
	}
	symbol = strings.ToUpper(strings.TrimSpace(symbol))

	days := 0
	if d, ok := params["days"].(float64); ok {
		days = int(d)
	}

	quote, err := s.fetchSinaQuote(symbol)
	if err != nil {
		return fmt.Sprintf("查询 %s 行情失败: %v", symbol, err), nil
	}

	if days > 0 {
		history, err := s.fetchSinaHistory(symbol, days)
		if err != nil {
			return quote + fmt.Sprintf("\n\n⚠️ 历史数据获取失败: %v", err), nil
		}
		return quote + "\n\n" + history, nil
	}

	return quote, nil
}

func (s *StockTool) fetchSinaQuote(symbol string) (string, error) {
	// 新浪财经美股接口：gb_aapl
	sinaSymbol := "gb_" + strings.ToLower(symbol)
	url := fmt.Sprintf("https://hq.sinajs.cn/list=%s", sinaSymbol)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %v", err)
	}

	content := string(body)
	// 响应格式: var hq_str_gb_aapl="苹果,198.15,3.52,1.69,198.49,199.62,...";
	startIdx := strings.Index(content, "\"")
	endIdx := strings.LastIndex(content, "\"")
	if startIdx == -1 || endIdx <= startIdx {
		return "", fmt.Errorf("未找到 %s 的行情数据，请确认股票代码正确", symbol)
	}

	dataStr := content[startIdx+1 : endIdx]
	if dataStr == "" {
		return "", fmt.Errorf("未找到 %s 的行情数据，请确认股票代码正确", symbol)
	}

	fields := strings.Split(dataStr, ",")
	if len(fields) < 11 {
		return "", fmt.Errorf("数据格式异常，字段数不足: %d", len(fields))
	}

	// 新浪美股实际字段顺序:
	// 0:名称 1:最新价 2:涨跌幅(%) 3:日期时间 4:涨跌额
	// 5:开盘价 6:最高价 7:最低价 8:昨收价 9:成交额 10:成交量
	name := fields[0]
	price := parseFloat(fields[1])
	changePercent := fields[2]
	tradeTime := fields[3]
	change := parseFloat(fields[4])
	open := parseFloat(fields[5])
	high := parseFloat(fields[6])
	low := parseFloat(fields[7])
	prevClose := parseFloat(fields[8])
	volume := fields[10]

	direction := "📈"
	if change < 0 {
		direction = "📉"
	}

	result := fmt.Sprintf(`%s %s (%s) 实时行情:
- 当前价格: $%.2f
- 今日开盘: $%.2f
- 最高价: $%.2f
- 最低价: $%.2f
- 昨收价: $%.2f
- 涨跌额: $%.2f
- 涨跌幅: %s%%
- 成交量: %s
- 交易时间: %s`,
		direction, name, symbol,
		price,
		open,
		high,
		low,
		prevClose,
		change,
		changePercent,
		formatVolume(volume),
		tradeTime,
	)

	return result, nil
}

func parseFloat(s string) float64 {
	s = strings.TrimSpace(s)
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// fetchSinaHistory 使用东方财富接口获取近N天日K线数据（国内可访问）
// 实际字段顺序: f51=日期,f52=开盘,f53=收盘,f54=最高,f55=最低,f56=成交量
func (s *StockTool) fetchSinaHistory(symbol string, days int) (string, error) {
	// 不加 lmt，取全量数据后截取最后 N 条（lmt 从最早开始计，无法取最新N条）
	url := fmt.Sprintf(
		"https://push2his.eastmoney.com/api/qt/stock/kline/get?secid=105.%s&fields1=f1,f2,f3,f4,f5,f6&fields2=f51,f52,f53,f54,f55,f56&klt=101&fqt=1&beg=0&end=20500101",
		strings.ToUpper(symbol),
	)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %v", err)
	}

	var result struct {
		Data struct {
			Klines []string `json:"klines"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("解析响应失败: %v", err)
	}
	if len(result.Data.Klines) == 0 {
		return "", fmt.Errorf("未找到 %s 的历史数据，请确认股票代码正确", symbol)
	}

	// 取最后 days 条
	klines := result.Data.Klines
	if len(klines) > days {
		klines = klines[len(klines)-days:]
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("📊 %s 近%d个交易日K线数据:\n", symbol, len(klines)))
	sb.WriteString("日期        | 开盘     | 最高     | 最低     | 收盘     | 成交量\n")
	sb.WriteString("------------|----------|----------|----------|----------|----------\n")

	for _, line := range klines {
		// 实际字段: 日期,开盘,收盘,最高,最低,成交量,...
		parts := strings.Split(line, ",")
		if len(parts) < 6 {
			continue
		}
		date := parts[0]  // 日期
		open := parts[1]  // 开盘
		close := parts[2] // 收盘
		high := parts[3]  // 最高
		low := parts[4]   // 最低
		vol := parts[5]   // 成交量
		sb.WriteString(fmt.Sprintf("%s | $%-8s | $%-8s | $%-8s | $%-8s | %s\n",
			date, open, high, low, close, formatVolume(vol)))
	}

	return sb.String(), nil
}

func formatVolume(s string) string {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return s
	}
	if v >= 100000000 {
		return fmt.Sprintf("%.2f亿", float64(v)/100000000)
	}
	if v >= 10000 {
		return fmt.Sprintf("%.2f万", float64(v)/10000)
	}
	return s
}
