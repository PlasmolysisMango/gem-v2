// Command gem-cli is the terminal client: online play against the gem-server
// (same HTTP/WS protocol as the web client) or offline play against local AIs.
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"gem/internal/ai"
	"gem/internal/cli"
	"gem/internal/game"
)

const usage = `璀璨宝石终端客户端

用法：
  gem-cli login [-server URL] [-register] <用户名> [密码]   登录/注册并保存 token 到 ~/.config/gem/token
  gem-cli rooms                                            列出大厅房间
  gem-cli create [-seats N] [-ai hard,medium] [-hints auto|on|off] [-timer 秒] [-name 名称]
                                                           创建房间并进入
  gem-cli join <房间号>                                     加入房间（对局中则观战/重连）
  gem-cli play [-offline] [-ai hard[,medium...]] [-seed N] [-budget 1.5s] [-review=true]
                                                           练习：-offline 本地对战 AI，否则在服务器开练习房

环境变量：GEM_SERVER 覆盖服务器地址，GEM_CONFIG_DIR 覆盖配置目录。
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	var err error
	args := os.Args[2:]
	switch os.Args[1] {
	case "login":
		err = cmdLogin(args)
	case "rooms":
		err = cmdRooms()
	case "create":
		err = cmdCreate(args)
	case "join":
		err = cmdJoin(args)
	case "play":
		err = cmdPlay(args)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "未知命令 %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误：", err)
		os.Exit(1)
	}
}

func cmdLogin(args []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	c := cli.LoadClient()
	server := fs.String("server", c.Server, "服务器地址")
	register := fs.Bool("register", false, "注册新账号")
	fs.Parse(args)
	if fs.NArg() < 1 {
		return fmt.Errorf("用法：gem-cli login [-server URL] [-register] <用户名> [密码]")
	}
	user, pass := fs.Arg(0), fs.Arg(1)
	if pass == "" {
		fmt.Print("密码：")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		pass = strings.TrimSpace(line)
	}
	c.Server, c.Token = *server, ""
	if err := c.Login(user, pass, *register); err != nil {
		return err
	}
	if err := c.Save(); err != nil {
		return err
	}
	fmt.Printf("已登录为 %s（%s），token 已保存到 %s\n", c.User.Username, c.Server, cli.ConfigDir())
	return nil
}

func cmdRooms() error {
	c := cli.LoadClient()
	rooms, err := c.Rooms()
	if err != nil {
		return err
	}
	if len(rooms) == 0 {
		fmt.Println("大厅暂无房间，使用 gem-cli create 创建")
		return nil
	}
	status := map[string]string{"waiting": "等待中", "playing": "对局中", "finished": "已结束"}
	fmt.Printf("%-8s %-16s %-10s %-6s %s\n", "房间号", "名称", "房主", "状态", "座位")
	for _, r := range rooms {
		hints := ""
		if r.Hints {
			hints = " 提示"
		}
		fmt.Printf("%-8s %-16s %-10s %-6s 真人%d AI%d 空%d/%d%s\n", r.ID, r.Name, r.OwnerName, status[r.Status], r.Humans, r.AIs, r.Open, r.Seats, hints)
	}
	return nil
}

func parseLevels(s string) ([]string, error) {
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := ai.ParseLevel(p); !ok {
			return nil, fmt.Errorf("未知 AI 难度 %q（easy/medium/hard）", p)
		}
		out = append(out, p)
	}
	return out, nil
}

func cmdCreate(args []string) error {
	fs := flag.NewFlagSet("create", flag.ExitOnError)
	seats := fs.Int("seats", 0, "座位数 2-4（默认 = 1 + AI 数，至少 2）")
	ais := fs.String("ai", "", "AI 座位难度，逗号分隔，如 hard,medium")
	hints := fs.String("hints", "auto", "提示：auto（全 AI 房开启）/on/off")
	timer := fs.Int("timer", 0, "回合计时（秒，0 关闭）")
	name := fs.String("name", "", "房间名")
	fs.Parse(args)
	levels, err := parseLevels(*ais)
	if err != nil {
		return err
	}
	set := game.RoomSettings{Name: *name, Seats: *seats, AIs: levels, TurnSeconds: *timer}
	if set.Seats == 0 {
		set.Seats = max(2, 1+len(levels))
	}
	switch *hints {
	case "on", "off":
		v := *hints == "on"
		set.Hints = &v
	case "auto":
	default:
		return fmt.Errorf("-hints 取值 auto/on/off")
	}
	return createAndJoin(set)
}

func createAndJoin(set game.RoomSettings) error {
	c := cli.LoadClient()
	room, err := c.CreateRoom(set)
	if err != nil {
		return err
	}
	fmt.Printf("已创建房间 %s\n", room.ID)
	return joinRoom(c, room.ID)
}

func cmdJoin(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("用法：gem-cli join <房间号>")
	}
	return joinRoom(cli.LoadClient(), strings.ToUpper(strings.TrimSpace(args[0])))
}

func joinRoom(c *cli.Client, id string) error {
	if _, err := c.Me(); err != nil {
		return err
	}
	be, err := c.Dial(context.Background(), id)
	if err != nil {
		return err
	}
	return run(be)
}

func cmdPlay(args []string) error {
	fs := flag.NewFlagSet("play", flag.ExitOnError)
	offline := fs.Bool("offline", false, "本地离线对战，无需服务器")
	ais := fs.String("ai", "hard", "对手难度，逗号分隔（1-3 个）")
	seed := fs.Uint64("seed", 0, "离线对局随机种子（0 为随机）")
	budget := fs.Duration("budget", 1500*time.Millisecond, "离线困难 AI / 提示的思考时间")
	review := fs.Bool("review", true, "离线：每步走后复盘")
	fs.Parse(args)
	levels, err := parseLevels(*ais)
	if err != nil {
		return err
	}
	if len(levels) < 1 || len(levels) > 3 {
		return fmt.Errorf("需要 1-3 个 AI 对手")
	}
	if !*offline {
		on := true
		return createAndJoin(game.RoomSettings{Name: "终端练习", Seats: 1 + len(levels), AIs: levels, Hints: &on})
	}
	ls := make([]ai.Level, len(levels))
	for i, l := range levels {
		ls[i] = ai.Level(l)
	}
	if *seed == 0 {
		*seed = rand.Uint64()
	}
	be, err := cli.NewOffline(ls, *seed, *budget)
	if err != nil {
		return err
	}
	be.Review = *review
	return run(be)
}

func run(be cli.Backend) error {
	p := tea.NewProgram(cli.NewModel(be), tea.WithAltScreen())
	be.Start(p.Send)
	_, err := p.Run()
	be.Close()
	return err
}
