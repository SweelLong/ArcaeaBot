package utils

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"

	"arcaeabot/internal/config"
	"arcaeabot/internal/database"
	"arcaeabot/internal/napcat"
	"arcaeabot/internal/onebot"
)

type Handler func(context.Context, napcat.Event, string) error
type Filter func(context.Context, napcat.Event) error

type Command struct {
	Name        string
	Aliases     []string
	Description string
	Handle      Handler
}

type Plugin interface {
	Name() string
	Commands() []Command
	GroupFilters() []Filter
	PrivateFilters() []Filter
}

type Context struct {
	Client    *napcat.Client
	Store     *database.Store
	Arcaea    *database.DB
	ArcaeaLog *database.DB
	Config    *config.Config
	Registry  *Registry
}

type Registry struct {
	ctx      Context
	plugins  []Plugin
	commands []commandEntry
	public   map[string]struct{}
	groups   map[int64]struct{}
	loaded   bool
}

type commandEntry struct {
	command Command
	trigger string
}

type factory struct {
	name    string
	aliases []string
	new     func(Context) Plugin
}

var factories []factory

func Add[T Plugin](name string, constructor func(Context) T, aliases ...string) {
	factories = append(factories, factory{
		name:    name,
		aliases: aliases,
		new: func(ctx Context) Plugin {
			return constructor(ctx)
		},
	})
}

func NewRegistry(client *napcat.Client, db *database.Store, arcaeaDB, arcaeaLogDB *database.DB, cfg *config.Config) *Registry {
	r := &Registry{}
	r.ctx = Context{Client: client, Store: db, Arcaea: arcaeaDB, ArcaeaLog: arcaeaLogDB, Config: cfg, Registry: r}
	return r
}

func (r *Registry) Context() Context {
	return r.ctx
}

func (r *Registry) Load() {
	r.plugins = nil
	r.commands = nil
	r.public = make(map[string]struct{})
	r.groups = make(map[int64]struct{})
	r.loaded = false
	available := append([]factory(nil), factories...)
	sort.Slice(available, func(i, j int) bool { return available[i].name < available[j].name })
	known := make(map[string]struct{}, len(available))
	for _, item := range available {
		known[item.name] = struct{}{}
		enabled := r.ctx.Config.PluginEnabled(item.name)
		for _, alias := range item.aliases {
			known[strings.ToLower(alias)] = struct{}{}
			enabled = enabled || r.ctx.Config.PluginEnabled(alias)
		}
		if enabled {
			r.Register(item.new(r.ctx))
		}
	}
	for _, name := range r.ctx.Config.EnabledPlugins {
		if name == "*" || strings.EqualFold(name, "none") {
			continue
		}
		if _, ok := known[strings.ToLower(name)]; !ok {
			slog.Warn("unknown plugin in enabled_plugins", "name", name)
		}
	}
	r.buildAccessIndex()
	r.loaded = true
}

func (r *Registry) buildAccessIndex() {
	for _, groupID := range r.ctx.Config.GroupWhitelist {
		if groupID != 0 {
			r.groups[groupID] = struct{}{}
		}
	}
	if len(r.groups) == 0 {
		slog.Warn("group whitelist is empty; all group events are blocked")
	}

	knownCommands := make(map[string]struct{})
	for _, plugin := range r.plugins {
		for _, command := range plugin.Commands() {
			for _, trigger := range append([]string{command.Name}, command.Aliases...) {
				trigger = strings.TrimSpace(trigger)
				if trigger == "" {
					continue
				}
				if _, exists := knownCommands[trigger]; exists {
					slog.Warn("duplicate command trigger", "trigger", trigger, "plugin", plugin.Name())
					continue
				}
				knownCommands[trigger] = struct{}{}
				r.commands = append(r.commands, commandEntry{command: command, trigger: trigger})
			}
		}
	}
	sort.Slice(r.commands, func(i, j int) bool {
		return len(r.commands[i].trigger) > len(r.commands[j].trigger)
	})
	for _, command := range r.ctx.Config.PublicCommands {
		command = strings.TrimSpace(command)
		if command == "" {
			continue
		}
		if _, ok := knownCommands[command]; !ok {
			slog.Warn("unknown public command", "command", command)
			continue
		}
		r.public[command] = struct{}{}
	}
}

func (r *Registry) Register(p Plugin) {
	r.plugins = append(r.plugins, p)
	slog.Info("plugin loaded", "name", p.Name())
}

func (r *Registry) Count() int {
	return len(r.plugins)
}

func (r *Registry) Plugins() []Plugin {
	return append([]Plugin(nil), r.plugins...)
}

func (r *Registry) Handle(ctx context.Context, event napcat.Event) error {
	if !r.loaded {
		return errors.New("plugin registry is not loaded")
	}
	if groupID := onebot.GroupID(event); groupID != 0 && !r.groupAllowed(groupID) {
		return nil
	}
	isMessage := onebot.IsGroupMessage(event) || onebot.IsPrivateMessage(event)
	if isMessage && !r.userBound(ctx, event) {
		entry, args, ok := r.matchCommand(strings.TrimSpace(onebot.RawMessage(event)))
		if !ok {
			return nil
		}
		if _, allowed := r.public[entry.trigger]; !allowed {
			return onebot.Reply(ctx, r.ctx.Client, event, "请先注册或绑定账号！(#注册 [用户名] [密码] / #绑定 [好友码])")
		}
		return entry.command.Handle(ctx, event, args)
	}
	for _, p := range r.plugins {
		if onebot.IsGroupMessage(event) || event.String("post_type") == "notice" {
			for _, filter := range p.GroupFilters() {
				if err := filter(ctx, event); err != nil {
					slog.Error("group filter", "plugin", p.Name(), "error", err)
				}
			}
		}
		if onebot.IsPrivateMessage(event) {
			for _, filter := range p.PrivateFilters() {
				if err := filter(ctx, event); err != nil {
					slog.Error("private filter", "plugin", p.Name(), "error", err)
				}
			}
		}
		if err := r.handleCommand(ctx, p, event); err != nil {
			slog.Error("command", "plugin", p.Name(), "error", err)
		}
	}
	return nil
}

func (r *Registry) userBound(ctx context.Context, event napcat.Event) bool {
	userID, err := UserID(ctx, r.ctx.Arcaea, onebot.UserID(event))
	if err != nil {
		return false
	}
	return ExistsRow(ctx, r.ctx.Arcaea, "SELECT 1 FROM user WHERE user_id=?", userID)
}

func (r *Registry) matchCommand(msg string) (commandEntry, string, bool) {
	for _, entry := range r.commands {
		if msg == entry.trigger {
			return entry, "", true
		}
		if strings.HasPrefix(msg, entry.trigger+" ") {
			return entry, strings.TrimSpace(strings.TrimPrefix(msg, entry.trigger)), true
		}
	}
	return commandEntry{}, "", false
}

func (r *Registry) groupAllowed(groupID int64) bool {
	if groupID == 0 {
		return true
	}
	_, ok := r.groups[groupID]
	return ok
}

func (r *Registry) handleCommand(ctx context.Context, p Plugin, event napcat.Event) error {
	if !onebot.IsGroupMessage(event) && !onebot.IsPrivateMessage(event) {
		return nil
	}
	msg := strings.TrimSpace(onebot.RawMessage(event))
	if msg == "" {
		return nil
	}
	for _, cmd := range p.Commands() {
		if msg == cmd.Name {
			return cmd.Handle(ctx, event, "")
		}
		for _, alias := range cmd.Aliases {
			if msg == alias {
				return cmd.Handle(ctx, event, "")
			}
		}
	}
	for _, cmd := range p.Commands() {
		if strings.HasPrefix(msg, cmd.Name+" ") {
			return cmd.Handle(ctx, event, strings.TrimSpace(strings.TrimPrefix(msg, cmd.Name)))
		}
		for _, alias := range cmd.Aliases {
			if strings.HasPrefix(msg, alias+" ") {
				return cmd.Handle(ctx, event, strings.TrimSpace(strings.TrimPrefix(msg, alias)))
			}
		}
	}
	return nil
}

type Base struct {
	PluginName string
	Cmds       []Command
	Groups     []Filter
	Privates   []Filter
}

func (b *Base) Init(name string) {
	b.PluginName = name
}

func (b *Base) Command(name string, aliases []string, handler Handler, descriptions ...string) {
	description := ""
	if len(descriptions) > 0 {
		description = descriptions[0]
	}
	b.Cmds = append(b.Cmds, Command{Name: name, Aliases: aliases, Description: description, Handle: handler})
}

func (b *Base) GroupFilter(filter Filter) {
	b.Groups = append(b.Groups, filter)
}

func (b *Base) PrivateFilter(filter Filter) {
	b.Privates = append(b.Privates, filter)
}

func (b *Base) Name() string {
	return b.PluginName
}

func (b *Base) Commands() []Command {
	return b.Cmds
}

func (b *Base) GroupFilters() []Filter {
	return b.Groups
}

func (b *Base) PrivateFilters() []Filter {
	return b.Privates
}
