//go:build linux

package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConsentPerComputer(t *testing.T) {
	lay := portableLayout(t)
	c := newFSConsent(lay, false)
	c.computer = "pc-a"
	if c.state() != "ask" || c.on() {
		t.Fatal("a stick asks first")
	}
	must(t, c.set(true))
	if !c.on() {
		t.Fatal("yes was not kept")
	}
	b := newFSConsent(lay, false)
	b.computer = "pc-b"
	if b.state() != "ask" {
		t.Fatal("another computer must be asked again")
	}
	must(t, b.set(false))
	if b.state() != "off" || !c.on() {
		t.Fatal("each computer keeps its own answer")
	}
	if id := computerID(); id != "" && (newFSConsent(lay, false).computer == id || len(newFSConsent(lay, false).computer) != 24) {
		t.Fatal("the computer's ID must be kept only as a hash")
	}
	none := newFSConsent(lay, false)
	none.computer = ""
	if none.state() != "off" {
		t.Fatal("without a computer ID it is never on")
	}
	inst := newFSConsent(installedLayout(t), false)
	inst.computer = "pc-a"
	if inst.state() != "on" {
		t.Fatal("the desktop apps are on by default")
	}
	must(t, inst.set(false))
	if inst.on() {
		t.Fatal("the desktop apps can be turned off")
	}
	usb := newFSConsent(lay, true)
	must(t, usb.set(false))
	if !usb.on() {
		t.Fatal("orOS USB is always on")
	}
}

func TestFSConsentGatesTheAPI(t *testing.T) {
	w := newFSWorld(t)
	lay := portableLayout(t)
	w.c.consent = newFSConsent(lay, false)
	w.c.consent.computer = "pc"
	if rec := w.req("GET", fsPrefix+"roots", nil); rec.Code != 404 || !strings.Contains(rec.Body.String(), `"off"`) {
		t.Fatalf("roots before a yes: %d %s", rec.Code, rec.Body)
	}
	rec := w.req("GET", fsPrefix+"consent", nil)
	if !strings.Contains(rec.Body.String(), `"state":"ask"`) {
		t.Fatalf("consent: %s", rec.Body)
	}
	// Only orOS may answer the question: the same checks as every call.
	r2 := w.req("POST", fsPrefix+"consent", strings.NewReader(`{"on":true}`), func(r *http.Request) { r.Header.Set("Sec-Fetch-Site", "cross-site") })
	if r2.Code != 403 || w.c.on() {
		t.Fatalf("another site said yes: %d", r2.Code)
	}
	if rec := w.req("POST", fsPrefix+"consent", strings.NewReader(`{}`)); rec.Code == 200 {
		t.Fatal("an empty answer was taken")
	}
	if rec := w.req("POST", fsPrefix+"consent", strings.NewReader(`{"on":true}`)); rec.Code != 200 || !w.c.on() {
		t.Fatalf("yes: %d %s", rec.Code, rec.Body)
	}
	if rec := w.req("GET", fsPrefix+"roots", nil); rec.Code != 200 {
		t.Fatalf("roots after yes: %d", rec.Code)
	}
	w.req("POST", fsPrefix+"consent", strings.NewReader(`{"on":false}`))
	if rec := w.req("GET", fsPrefix+"list?path=/home", nil); rec.Code != 404 {
		t.Fatal("off must close the API again")
	}
}

func TestFSPrivateFolders(t *testing.T) {
	w := newFSWorld(t)
	stick := filepath.Join(w.disk, "orOS-Portable")
	must(t, os.MkdirAll(filepath.Join(stick, "data", "profile"), 0o755))
	must(t, os.WriteFile(filepath.Join(w.disk, "photo.jpg"), []byte("x"), 0o644))
	must(t, os.Symlink(stick, filepath.Join(w.home, "Documents", "link")))
	w.c.private = realPaths(stick)
	l, code := w.list("/disks/STICK")
	if code != 200 || names(l) != "photo.jpg" {
		t.Fatalf("the orOS Portable folder must not show: %s", names(l))
	}
	if _, code := w.list("/disks/STICK/orOS-Portable/data"); code == 200 {
		t.Fatal("the data folder was reachable")
	}
	if l, _ := w.list("/home/Documents"); strings.Contains(names(l), "link") {
		t.Fatal("a link to a private folder was listed")
	}
	if rec := w.put("/disks/STICK/orOS-Portable/x.txt", "x", ""); rec.Code == 200 {
		t.Fatal("a write reached the private folder")
	}
}

const lsblkSample = `{"blockdevices":[
 {"name":"nvme0n1","path":"/dev/nvme0n1","pkname":null,"type":"disk","size":512110190592,"fstype":null,"label":null,"mountpoint":null,"rm":false,"hotplug":false,"tran":"nvme","model":"Samsung SSD"},
 {"name":"nvme0n1p1","path":"/dev/nvme0n1p1","pkname":"nvme0n1","type":"part","size":536870912,"fstype":"vfat","label":null,"mountpoint":"/boot/efi","rm":false,"hotplug":false,"tran":null,"model":null},
 {"name":"nvme0n1p2","path":"/dev/nvme0n1p2","pkname":"nvme0n1","type":"part","size":300000000000,"fstype":"ext4","label":null,"mountpoint":"/","rm":false,"hotplug":false,"tran":null,"model":null},
 {"name":"nvme0n1p3","path":"/dev/nvme0n1p3","pkname":"nvme0n1","type":"part","size":200000000000,"fstype":"ext4","label":"data","mountpoint":null,"rm":false,"hotplug":false,"tran":null,"model":null},
 {"name":"sda","path":"/dev/sda","pkname":null,"type":"disk","size":1000204886016,"fstype":null,"label":null,"mountpoint":null,"rm":false,"hotplug":false,"tran":"sata","model":"WD Blue"},
 {"name":"sda1","path":"/dev/sda1","pkname":"sda","type":"part","size":1000000000000,"fstype":"ntfs","label":"Windows","mountpoint":null,"rm":false,"hotplug":false,"tran":null,"model":null},
 {"name":"sdb","path":"/dev/sdb","pkname":null,"type":"disk","size":32010928128,"fstype":null,"label":null,"mountpoint":null,"rm":true,"hotplug":true,"tran":"usb","model":"SanDisk"},
 {"name":"sdb1","path":"/dev/sdb1","pkname":"sdb","type":"part","size":32000000000,"fstype":"exfat","label":"STICK","mountpoint":"/run/media/chris/STICK","rm":true,"hotplug":true,"tran":null,"model":null},
 {"name":"sdc1","path":"/dev/sdc1","pkname":"sdc","type":"part","size":64000000000,"fstype":"ext4","label":"backup","mountpoint":"/mnt/backup","rm":false,"hotplug":false,"tran":null,"model":null}
]}`

func TestPickHostVolumes(t *testing.T) {
	vs, err := pickHostVolumes(lsblkSample, "/run/media/chris")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range vs {
		got = append(got, v.Path)
	}
	// The system disk's other partitions, small ones and disks the computer
	// uses elsewhere (/mnt/backup) stay out.
	if strings.Join(got, ",") != "/dev/sda1,/dev/sdb1" {
		t.Fatalf("volumes: %v", got)
	}
	if !vs[0].Internal || vs[0].USB || vs[0].Model != "WD Blue" || !vs[1].USB || vs[1].Mount != "/run/media/chris/STICK" {
		t.Fatalf("%+v", vs)
	}
	b, _ := json.Marshal(vs[1])
	for _, k := range []string{`"path"`, `"label"`, `"fstype"`, `"size"`, `"usb"`, `"mount"`, `"internal"`} {
		if !strings.Contains(string(b), k) {
			t.Fatalf("oros-system's field %s missing: %s", k, b)
		}
	}
}

func TestHostMountThroughUdisks(t *testing.T) {
	oldL, oldU := lsblkJSON, udisksctl
	defer func() { lsblkJSON, udisksctl = oldL, oldU }()
	lsblkJSON = func() (string, error) {
		return strings.Replace(lsblkSample, `"/run/media/chris/STICK"`, "null", 1), nil
	}
	var calls []string
	udisksctl = func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "mount" {
			return "Mounted " + args[2] + " at " + hostMedia() + "/STICK\n", nil
		}
		return "Unmounted\n", nil
	}
	dir, err := hostMount("/dev/sdb1", false)
	if err != nil || dir != hostMedia()+"/STICK" {
		t.Fatalf("%q %v", dir, err)
	}
	if _, err := hostMount("/dev/sda1", false); err != nil {
		t.Fatal(err)
	}
	if calls[1] != "mount -b /dev/sda1 -o ro" || strings.Contains(calls[0], "ro") {
		t.Fatalf("Windows disks open read-only, sticks writable: %v", calls)
	}
	if _, err := hostMount("/dev/nvme0n1p2", false); err == nil {
		t.Fatal("the system disk was opened")
	}
}
