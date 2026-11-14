//go:build qt

package main

import (
	"fmt"
	"path/filepath"
	"strings"

	qt "github.com/mappu/miqt/qt6"
	"github.com/mappu/miqt/qt6/mainthread"

	"github.com/panedrone/go-upnp-player/internal/config"
	"github.com/panedrone/go-upnp-player/internal/library"
	"github.com/panedrone/go-upnp-player/internal/player"
)

// search is the library's search: a box over the tree, and the results in place of the tree
// while it has text, as on the page.
type search struct {
	box     *qt.QLineEdit
	results *qt.QWidget
	all     *qt.QPushButton
	hits    *qt.QTreeWidget

	ids    []string // the results' rows
	leaves []string
	seq    int
}

// buildLibrary is the left column: the search box over the tree or over the results.
func (u *window) buildLibrary() *qt.QWidget {
	box := qt.NewQWidget2()
	col := qt.NewQVBoxLayout(box)
	col.SetContentsMargins(0, 0, 0, 0)
	u.find.box = qt.NewQLineEdit2()
	u.find.box.SetPlaceholderText("Search artist, album, title…")
	u.find.box.SetClearButtonEnabled(true)
	timer := qt.NewQTimer()
	timer.SetSingleShot(true)
	timer.OnTimeout(u.runSearch)
	u.find.box.OnTextChanged(func(string) { timer.Start(250) }) // a pause in typing, as the page waits
	// the search box in place of a title, then refresh and Collapse All, as on the page
	row := qt.NewQHBoxLayout2()
	row.AddWidget(u.find.box.QWidget)
	u.rescan = qt.NewQPushButton3("⟳")
	square(u.rescan)
	u.rescan.SetToolTip("Refresh: rescan the library folders")
	u.rescan.OnClicked(func() {
		u.rescan.SetEnabled(false) // until the scan is done (show)
		go library.Scan(config.Get().Roots)
	})
	row.AddWidget(u.rescan.QWidget)
	collapse := qt.NewQPushButton3("⇈")
	square(collapse)
	collapse.SetToolTip("Collapse all folders")
	collapse.OnClicked(u.collapseAll)
	row.AddWidget(collapse.QWidget)
	col.AddLayout(row.QLayout)
	col.AddWidget(u.buildTree())
	col.AddWidget(u.buildResults())
	return box
}

func (u *window) buildResults() *qt.QWidget {
	f := &u.find
	f.results = qt.NewQWidget2()
	col := qt.NewQVBoxLayout(f.results)
	col.SetContentsMargins(0, 0, 0, 0)
	f.all = qt.NewQPushButton3("")
	f.all.OnClicked(func() {
		player.SetQueue(append([]string(nil), f.ids...), "")
		player.Notify()
	})
	col.AddWidget(f.all.QWidget)
	f.hits = qt.NewQTreeWidget2()
	f.hits.SetColumnCount(4)
	f.hits.SetHeaderHidden(true)
	f.hits.SetRootIsDecorated(false)
	f.hits.SetUniformRowHeights(true)
	f.hits.Header().SetStretchLastSection(false)
	f.hits.Header().SetSectionResizeMode2(0, qt.QHeaderView__ResizeToContents)
	f.hits.Header().SetSectionResizeMode2(1, qt.QHeaderView__Stretch)
	f.hits.Header().SetSectionResizeMode2(2, qt.QHeaderView__ResizeToContents)
	f.hits.Header().SetSectionResizeMode2(3, qt.QHeaderView__Fixed)
	f.hits.SetColumnWidth(3, 28)
	// → makes a playlist of the track; a click elsewhere shows it in the tree and leaves the search
	f.hits.OnItemClicked(func(item *qt.QTreeWidgetItem, column int) {
		i := f.hits.IndexOfTopLevelItem(item)
		if i < 0 || i >= len(f.ids) {
			return
		}
		if column == 3 {
			player.SetQueue([]string{f.ids[i]}, "")
			player.Notify()
			return
		}
		leaf := f.leaves[i]
		f.box.SetText("")
		u.showTree()
		u.reveal(leaf)
	})
	col.AddWidget(f.hits.QWidget)
	f.results.Hide()
	return f.results
}

func (u *window) showTree() {
	u.find.results.Hide()
	u.tree.Show()
}

// runSearch looks the text up off the GUI thread; an answer to an older text is dropped.
func (u *window) runSearch() {
	f := &u.find
	text := strings.TrimSpace(f.box.Text())
	f.seq++
	seq := f.seq
	if text == "" {
		u.showTree()
		return
	}
	go func() {
		found := library.Search(text, 2000)
		mainthread.Start(func() {
			if seq == f.seq {
				u.showResults(found)
			}
		})
	}()
}

func (u *window) showResults(found []*library.Track) {
	f := &u.find
	f.hits.Clear()
	f.ids, f.leaves = f.ids[:0], f.leaves[:0]
	for _, t := range found {
		num := ""
		if t.Number > 0 {
			num = fmt.Sprint(t.Number)
		}
		text := t.Title
		if who := strings.Join(nonEmpty(t.Artist, t.Album), " · "); who != "" {
			text += "  —  " + who
		}
		item := qt.NewQTreeWidgetItem2([]string{num, text, fmtTime(t.Duration), "→"})
		item.SetTextAlignment(0, int(qt.AlignRight|qt.AlignVCenter))
		item.SetToolTip(1, "Show in the tree: "+t.Leaf)
		item.SetToolTip(3, "Make a playlist of this track")
		f.hits.AddTopLevelItem(item)
		f.ids = append(f.ids, t.ID)
		f.leaves = append(f.leaves, t.Leaf)
	}
	if len(found) == 0 {
		f.all.SetText("Nothing found")
		f.all.SetEnabled(false)
	} else {
		f.all.SetText(fmt.Sprintf("→ Make a playlist of all %d results", len(found)))
		f.all.SetEnabled(true)
	}
	u.tree.Hide()
	f.results.Show()
}

// reveal opens the tree down to the node at path (a folder, a cue sheet or a file) and
// selects it, as the page's crosshair does.
func (u *window) reveal(path string) {
	var level []*qt.QTreeWidgetItem
	for i := range u.tree.TopLevelItemCount() {
		level = append(level, u.tree.TopLevelItem(i))
	}
	for len(level) > 0 {
		var next []*qt.QTreeWidgetItem
		for _, item := range level {
			p := item.Data(0, rolePath).ToString()
			if p == path {
				u.tree.SetCurrentItem(item)
				u.tree.ScrollToItem(item)
				return
			}
			if strings.HasPrefix(path, p+string(filepath.Separator)) || strings.HasPrefix(path, p+"#") {
				item.SetExpanded(true) // loads its children (OnItemExpanded)
				for c := range item.ChildCount() {
					next = append(next, item.Child(c))
				}
				break
			}
		}
		level = next
	}
}

// revealPlaylist shows in the tree what the playlist was built from, or the current track's
// cue sheet or file for a playlist built from search results, and leaves the search.
func (u *window) revealPlaylist() {
	target := u.source
	if target == "" {
		target = u.leaf
	}
	if target == "" {
		return
	}
	u.find.box.SetText("")
	u.showTree()
	u.reveal(target)
}

// collapseAll folds everything below the library roots and leaves the search; the roots stay
// open, or the tree would shrink to a single row.
func (u *window) collapseAll() {
	u.find.box.SetText("")
	u.showTree()
	u.tree.CollapseAll() // OnItemCollapsed forgets each folded node
	u.expanded = map[string]bool{}
	for i := range u.tree.TopLevelItemCount() {
		u.tree.TopLevelItem(i).SetExpanded(true)
	}
}

func nonEmpty(s ...string) []string {
	var out []string
	for _, v := range s {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
