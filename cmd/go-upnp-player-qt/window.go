//go:build qt

package main

import (
	"fmt"
	"strings"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/panedrone/go-upnp-player/internal/config"
	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/player"
)

// The data roles the tree keeps on its rows.
const (
	rolePath = int(qt.UserRole)     // the library node's path
	roleKind = int(qt.UserRole) + 1 // dir | cue | file | track
)

// window is the Qt main window: the library tree, the playlist with its transport and
// position, and the devices with the volume. It calls the player directly; a command runs
// on a goroutine, since many wait for the renderer for seconds, and the window follows the
// player's state through player.Subscribe.
type window struct {
	w          *qt.QMainWindow
	tree       *qt.QTreeWidget
	list       *qt.QTreeWidget
	devices    *qt.QListWidget
	play       *qt.QPushButton
	pos        *qt.QLabel
	dur        *qt.QLabel
	seek       *qt.QSlider
	vol        *qt.QSlider
	busy       *qt.QLabel
	errText    *qt.QLabel
	find       search
	props      props
	eq         eq
	folders    folders
	ids        []string  // the playlist's rows
	ui         config.UI // the look and layout as it was left (look.go)
	splits     map[string]*qt.QSplitter
	expanded   map[string]bool // the tree's open nodes, by path
	source     string          // the tree node the playlist was built from ("" for search results)
	leaf       string          // the current track's cue sheet or file
	revealed   bool            // the playlist's source was shown once on start
	rescan     *qt.QPushButton
	rediscover *qt.QPushButton
	stream     *qt.QLabel                 // what goes out and from where, in the status bar
	transport  map[string]*qt.QPushButton // the transport buttons, by icon name
	playing    bool                       // the play button shows pause
	note       *qt.QLabel                 // the update check's answer (update.go)
	noteClose  *qt.QPushButton

	queueVersion int
	scans        int
	devicesKey   string
	playingRow   int
	rows         []*qt.QTreeWidgetItem // the playlist's track rows, by queue index
	albums       []album               // the playlist's albums, in order
	head         *albumHead            // the album header over the playlist
	udns         []string              // the devices list's rows
}

func newWindow(quit func()) *window {
	u := &window{queueVersion: -1, scans: -1, playingRow: -1, ui: loadUI(), splits: map[string]*qt.QSplitter{}}
	applyTheme(u.ui.Theme)
	u.w = qt.NewQMainWindow2()
	u.w.SetWindowTitle("go-upnp-player Qt")
	if len(u.ui.Geometry) == 0 || !u.w.RestoreGeometry(u.ui.Geometry) {
		u.w.Resize(1360, 900)
	}

	split := qt.NewQSplitter3(qt.Horizontal)
	split.AddWidget(u.buildLibrary())
	mid := qt.NewQSplitter3(qt.Vertical) // the playlist, and its Properties under a splitter
	mid.AddWidget(u.buildPlaylist())
	mid.AddWidget(u.buildProperties())
	u.splitter("playlist", mid, []int{500, 400})
	split.AddWidget(mid.QWidget)
	split.AddWidget(u.buildDevices())
	u.splitter("columns", split, []int{420, 520, 360})
	u.w.SetCentralWidget(split.QWidget)

	u.buildNote()
	u.busy = qt.NewQLabel2()
	u.errText = qt.NewQLabel2()
	u.errText.SetStyleSheet("color: #dc3545")
	u.w.StatusBar().AddWidget(u.busy.QWidget)
	u.w.StatusBar().AddWidget(u.errText.QWidget)
	// what goes out and from where, a group of its own before the version (showStream)
	u.stream = qt.NewQLabel2()
	u.stream.SetStyleSheet("color: gray; padding-right: 12px")
	u.w.StatusBar().AddPermanentWidget(u.stream.QWidget)
	u.w.StatusBar().AddPermanentWidget(qt.NewQLabel3(player.Version).QWidget)

	// closing the window quits, as the web page's Quit does
	u.w.OnCloseEvent(func(super func(event *qt.QCloseEvent), event *qt.QCloseEvent) {
		u.saveLayout()
		quit()
		super(event)
	})
	u.setCursors()
	u.w.Show()
	u.follow()
	return u
}

func (u *window) raise() {
	u.w.ShowNormal()
	u.w.Raise()
	u.w.ActivateWindow()
}

// --- library tree

func (u *window) buildTree() *qt.QWidget {
	u.tree = qt.NewQTreeWidget2()
	u.tree.SetColumnCount(2)
	u.tree.SetHeaderHidden(true)
	u.tree.SetUniformRowHeights(true)
	u.tree.Header().SetStretchLastSection(false)
	u.tree.Header().SetSectionResizeMode2(0, qt.QHeaderView__Stretch)
	u.tree.Header().SetSectionResizeMode2(1, qt.QHeaderView__Fixed)
	u.tree.SetColumnWidth(1, 28)
	// the open nodes are remembered (look.go), as the page keeps them in its browser
	u.tree.OnItemExpanded(func(item *qt.QTreeWidgetItem) {
		path := item.Data(0, rolePath).ToString()
		if u.expanded == nil {
			u.expanded = map[string]bool{}
		}
		u.expanded[path] = true
		if item.ChildCount() == 0 {
			u.addNodes(item, library.Children(path))
		}
	})
	u.tree.OnItemCollapsed(func(item *qt.QTreeWidgetItem) {
		delete(u.expanded, item.Data(0, rolePath).ToString())
	})
	// → in the second column builds the playlist from the node; playback is left alone
	u.tree.OnItemClicked(func(item *qt.QTreeWidgetItem, column int) {
		if column != 1 {
			return
		}
		path := item.Data(0, rolePath).ToString()
		var ids []string
		for _, t := range library.TracksUnder(path) {
			ids = append(ids, t.ID)
		}
		player.SetQueue(ids, path)
		player.Notify()
	})
	return u.tree.QWidget
}

// loadTree draws the roots and opens the nodes that were open; on the first run, nothing
// saved yet, the roots are open.
func (u *window) loadTree() {
	u.tree.Clear()
	roots := library.Children("")
	// decided once there are roots: the first draw may come before the first scan is done
	if u.expanded == nil && (len(roots) > 0 || u.ui.Expanded != nil) {
		u.expanded = map[string]bool{}
		if u.ui.Expanded == nil {
			for _, n := range roots {
				u.expanded[n.Path] = true
			}
		}
		for _, p := range u.ui.Expanded {
			u.expanded[p] = true
		}
	}
	u.addNodes(nil, roots)
}

func (u *window) addNodes(parent *qt.QTreeWidgetItem, nodes []library.Node) {
	var open []*qt.QTreeWidgetItem
	for _, n := range nodes {
		item := qt.NewQTreeWidgetItem2([]string{n.Name, "→"})
		item.SetData(0, rolePath, qt.NewQVariant14(n.Path))
		item.SetData(0, roleKind, qt.NewQVariant14(n.Kind))
		item.SetToolTip(1, "Make a playlist of it")
		if n.Kind == "dir" {
			item.SetChildIndicatorPolicy(qt.QTreeWidgetItem__ShowIndicator)
		}
		if parent == nil {
			u.tree.AddTopLevelItem(item)
		} else {
			parent.AddChild(item)
		}
		if u.expanded[n.Path] {
			open = append(open, item)
		}
	}
	for _, item := range open {
		item.SetExpanded(true) // loads its children, which open in turn (OnItemExpanded)
	}
}

// --- playlist with its transport and position

func (u *window) buildPlaylist() *qt.QWidget {
	box := qt.NewQWidget2()
	col := qt.NewQVBoxLayout(box)
	col.SetContentsMargins(0, 0, 0, 0)

	head := qt.NewQHBoxLayout2()
	// the crosshair (Alt+F1): shows in the tree what the playlist was built from, as on the page
	find := qt.NewQPushButton3("⌖")
	square(find)
	find.SetToolTip("Show in the tree what the playlist was built from (Alt+F1)")
	find.OnClicked(u.revealPlaylist)
	qt.NewQShortcut2(qt.NewQKeySequence2("Alt+F1"), u.w.QObject).OnActivated(u.revealPlaylist)
	head.AddWidget(find.QWidget)
	u.pos = qt.NewQLabel3("0:00")
	u.seek = qt.NewQSlider3(qt.Horizontal)
	clickJumps(u.seek)
	u.seek.OnSliderReleased(func() {
		v := float64(u.seek.Value())
		u.run(func() error { return player.Seek(v) })
	})
	u.dur = qt.NewQLabel3("0:00")
	head.AddWidget(u.pos.QWidget)
	head.AddWidget(u.seek.QWidget)
	head.AddWidget(u.dur.QWidget)
	// the transport: the window's ordinary buttons, side by side as one group (icons.go)
	group := qt.NewQWidget2()
	pult := qt.NewQHBoxLayout(group)
	pult.SetContentsMargins(0, 0, 0, 0)
	pult.SetSpacing(0)
	u.transport = map[string]*qt.QPushButton{}
	for _, b := range []struct {
		name, tip string
		do        func() error
	}{{"prev", "Previous", player.Prev}, {"play", "Play", nil}, {"stop", "Stop", player.Stop}, {"next", "Next", player.Next}} {
		btn := qt.NewQPushButton2()
		btn.SetToolTip(b.tip)
		square(btn)
		btn.SetIconSize(qt.NewQSize2(14, 14))
		u.transport[b.name] = btn
		if b.do == nil {
			u.play = btn
			btn.OnClicked(func() {
				if player.Snapshot().Transport == "PLAYING" {
					u.run(player.Pause)
				} else {
					u.run(player.Resume)
				}
			})
		} else {
			do := b.do
			btn.OnClicked(func() { u.run(do) })
		}
		pult.AddWidget(btn.QWidget)
	}
	head.AddWidget(group)
	u.paintTransport()
	col.AddLayout(head.QLayout)

	u.list = qt.NewQTreeWidget2()
	u.list.SetColumnCount(4)
	u.list.SetHeaderHidden(true)
	u.list.SetRootIsDecorated(false)
	u.list.Header().SetStretchLastSection(false)
	u.list.Header().SetSectionResizeMode2(0, qt.QHeaderView__ResizeToContents)
	u.list.Header().SetSectionResizeMode2(1, qt.QHeaderView__Stretch)
	u.list.Header().SetSectionResizeMode2(2, qt.QHeaderView__ResizeToContents)
	u.list.Header().SetSectionResizeMode2(3, qt.QHeaderView__Fixed)
	u.list.SetColumnWidth(3, 28)
	// a click anywhere on a row plays it; the i after the duration shows its properties
	u.list.OnItemClicked(func(item *qt.QTreeWidgetItem, column int) {
		i, ok := rowIndex(item)
		if !ok {
			return // an album header
		}
		if column == 3 {
			if i >= 0 && i < len(u.ids) {
				u.props.picked = true
				u.showProps(u.ids[i])
			}
			return
		}
		u.run(func() error { return player.PlayIndex(i) })
	})
	// the album's header stays over the list while the tracks scroll (albums.go)
	u.head = newAlbumHead()
	col.AddWidget(u.head.box)
	u.list.VerticalScrollBar().OnValueChanged(func(int) { u.stickHead() })
	col.AddWidget(u.list.QWidget)
	return box
}

// --- devices and volume

func (u *window) buildDevices() *qt.QWidget {
	box := qt.NewQWidget2()
	col := qt.NewQVBoxLayout(box)
	col.SetContentsMargins(0, 0, 0, 0)

	vol := qt.NewQHBoxLayout2()
	vol.AddWidget(qt.NewQLabel3("Vol").QWidget)
	u.vol = qt.NewQSlider3(qt.Horizontal)
	clickJumps(u.vol)
	u.vol.SetRange(0, 100)
	u.vol.OnSliderReleased(func() {
		v := u.vol.Value()
		u.run(func() error { return player.SetVolume(v) })
	})
	vol.AddWidget(u.vol.QWidget)
	// search the network for renderers again; off while a search runs (show)
	u.rediscover = qt.NewQPushButton3("⟳")
	square(u.rediscover)
	u.rediscover.SetToolTip("Search for devices again")
	u.rediscover.OnClicked(func() {
		u.rediscover.SetEnabled(false)
		go func() {
			player.Discover()
			player.Notify()
		}()
	})
	vol.AddWidget(u.rediscover.QWidget)
	// the theme, as by the page's volume: the button shows the theme it switches to
	theme := qt.NewQPushButton2()
	square(theme)
	face := func() {
		text, tip := themeIcon(u.ui.Theme)
		theme.SetText(text)
		theme.SetToolTip(tip)
	}
	face()
	theme.OnClicked(func() {
		if u.ui.Theme == "light" {
			u.ui.Theme = "dark"
		} else {
			u.ui.Theme = "light"
		}
		applyTheme(u.ui.Theme)
		face()
		u.paintTransport()
		t := u.ui.Theme
		saveUI(func(ui *config.UI) { ui.Theme = t })
	})
	vol.AddWidget(theme.QWidget)
	// check for updates: GitHub is asked only on this click (update.go)
	check := qt.NewQPushButton3("⭳")
	square(check)
	check.SetToolTip("Check for updates")
	check.OnClicked(u.checkUpdate)
	vol.AddWidget(check.QWidget)
	col.AddLayout(vol.QLayout)

	col.AddWidget(qt.NewQLabel3("Output").QWidget)
	u.devices = qt.NewQListWidget2()
	// a click on a device uses it; playback goes on there
	u.devices.OnItemClicked(func(item *qt.QListWidgetItem) {
		row := u.devices.Row(item)
		if row < 0 || row >= len(u.udns) {
			return
		}
		udn := u.udns[row]
		u.run(func() error { return player.Select(udn) })
	})
	col.AddWidget(u.devices.QWidget)
	col.AddWidget(u.buildEQ())
	col.AddWidget(u.buildFolders())
	col.AddStretch()
	return box
}

// --- state

// run does a player command off the GUI thread; its error shows in the status bar.
func (u *window) run(cmd func() error) {
	go func() {
		err := cmd()
		player.Notify()
		mainthread.Start(func() {
			if err != nil {
				u.errText.SetText(err.Error())
			} else {
				u.errText.SetText("")
			}
		})
	}()
}

// follow keeps the window on the player's state: a wake after every poll of the renderer
// and every command (player.Subscribe), the state read and shown on the GUI thread.
func (u *window) follow() {
	u.show(player.Snapshot())
	wake, done, _ := player.Subscribe()
	go func() {
		for {
			select {
			case <-done:
				return
			case <-wake:
				s := player.Snapshot()
				mainthread.Start(func() { u.show(s) })
			}
		}
	}()
}

func (u *window) show(s player.State) {
	if s.Library.Scans != u.scans {
		u.scans = s.Library.Scans
		u.loadTree()
		u.queueVersion = -1 // a restored playlist finds its tracks only after the scan
	}
	u.source = s.Source
	if s.Track != nil {
		u.leaf = s.Track.Leaf
	}
	// the page shows the playlist's source in the tree once by itself when it opens
	if !u.revealed && u.tree.TopLevelItemCount() > 0 && (u.source != "" || u.leaf != "") {
		u.revealed = true
		u.revealPlaylist()
	}
	if s.QueueVersion != u.queueVersion {
		u.queueVersion = s.QueueVersion
		u.loadPlaylist()
	}
	playing := s.Transport == "PLAYING" && !s.Paused && !s.Stalled
	if playing != u.playing {
		u.playing = playing
		u.paintTransport()
		if playing {
			u.play.SetToolTip("Pause")
		} else {
			u.play.SetToolTip("Play")
		}
	}
	if s.WantPlaying || s.Paused || playing {
		u.markPlaying(s.Index)
	} else {
		u.markPlaying(-1)
	}
	// the properties follow the playing track until an i is clicked
	if !u.props.picked && s.Track != nil && s.Track.ID != u.props.id {
		u.showProps(s.Track.ID)
	}
	dur := s.Duration
	if dur == 0 && s.Track != nil {
		dur = s.Track.Duration
	}
	u.dur.SetText(fmtTime(dur))
	if !u.seek.IsSliderDown() {
		u.seek.SetRange(0, int(dur))
		u.seek.SetValue(int(s.Position))
		u.pos.SetText(fmtTime(s.Position))
	}
	u.seek.SetEnabled(s.Track != nil && !s.Off)
	if !u.vol.IsSliderDown() && s.Volume >= 0 {
		u.vol.SetValue(s.Volume)
	}
	u.showDevices(s)
	u.showStream(s)
	u.showLibrary(s.Library.Scanning, s.Library.Tracks)
	u.rescan.SetEnabled(!s.Library.Scanning)
	u.rediscover.SetEnabled(!s.Discovering)
	u.busy.SetText(s.Waiting)
	if s.Error != "" {
		u.errText.SetText(s.Error)
	}
}

func (u *window) showDevices(s player.State) {
	var key strings.Builder
	for _, d := range s.Renderers {
		key.WriteString(d.UDN + "|" + d.Name + ";")
	}
	key.WriteString(s.Selected + "|" + s.Transport)
	if key.String() == u.devicesKey {
		return
	}
	u.devicesKey = key.String()
	u.devices.Clear()
	u.udns = u.udns[:0]
	for _, d := range s.Renderers {
		text := d.IP + "  " + d.Name
		if d.UDN == s.Selected {
			text += "   [" + strings.ReplaceAll(s.Transport, "_PLAYBACK", "") + "]"
		}
		u.devices.AddItem(text)
		u.udns = append(u.udns, d.UDN)
	}
}

func fmtTime(sec float64) string {
	s := int(sec + 0.5)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// showStream puts in the status bar what goes out and from where, as the page's Output header:
// "from 192.168.0.101 • 44.1 kHz • 16 bit • stereo • cue → WAV".
func (u *window) showStream(s player.State) {
	var parts []string
	for _, d := range s.Renderers {
		if d.UDN == s.Selected && d.LocalIP != "" {
			parts = append(parts, "from "+d.LocalIP)
		}
	}
	if t := s.Track; t != nil {
		ch := "stereo"
		if t.Channels != 2 {
			ch = fmt.Sprintf("%d ch", t.Channels)
		}
		kind := "FLAC"
		if t.FromCue {
			kind = "cue → WAV"
		}
		parts = append(parts, fmt.Sprintf("%g kHz", float64(t.SampleRate)/1000), fmt.Sprintf("%d bit", t.Bits), ch, kind)
	}
	u.stream.SetText(strings.Join(parts, "  •  "))
}
