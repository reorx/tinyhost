package store

import (
 "errors"
 "fmt"
 "path/filepath"
 "sync"
 "testing"
 "time"
 "github.com/reorx/tinyhost/internal/bundle"
)
func openTest(t *testing.T)*Store{t.Helper();s,e:=Open(filepath.Join(t.TempDir(),"tinyhost.db"));if e!=nil{t.Fatal(e)};t.Cleanup(func(){s.Close()});return s}
func appTest(t *testing.T,s *Store)*App{t.Helper();a,e:=s.CreateApp("report","Title","public","c_a");if e!=nil{t.Fatal(e)};return a}
func layer(p,b string)*Layer{return &Layer{Files:[]File{{Path:p,Blob:b,Size:1}}}}
func deploy(t *testing.T,s *Store,a *App,b string,promote bool)*Version{t.Helper();v,e:=s.CommitAppVersion(a.ID,layer("index.html",b),bundle.Manifest{Title:b},"msg","c_a",promote);if e!=nil{t.Fatal(e)};return v}
// Behavior: SQLite uses WAL, foreign keys, a busy timeout, single connection and persistent idempotent migrations.
func TestDatabaseReopensWithAllSchemaAndPragmas(t *testing.T){p:=filepath.Join(t.TempDir(),"db");s,e:=Open(p);if e!=nil{t.Fatal(e)};for _,q:=range []struct{key string;want any}{{"journal_mode","wal"},{"foreign_keys",1},{"busy_timeout",5000},{"user_version",1}}{var v any;if e=s.db.QueryRow("PRAGMA "+q.key).Scan(&v);e!=nil{t.Fatal(e)};if fmt.Sprint(v)!=fmt.Sprint(q.want){t.Fatal(q.key,v)}};if s.db.Stats().MaxOpenConnections!=1{t.Fatal("connections")};a:=appTest(t,s);s.Close();s,e=Open(p);if e!=nil{t.Fatal(e)};defer s.Close();b,e:=s.AppBySlug(a.Slug);if e!=nil||b.ID!=a.ID{t.Fatal(b,e)}}
// Behavior: client records store only hashes, preserve expiry/revocation and throttle last-used updates to once per minute.
func TestClientsPersistCredentialsAndThrottleTouch(t *testing.T){s:=openTest(t);now:=int64(1000);s.now=func()time.Time{return time.Unix(now,0)};expiry:=int64(2000);c,e:=s.CreateClient("agent","hash",&expiry);if e!=nil{t.Fatal(e)};if _,e=s.CreateClient("duplicate","hash",nil);e==nil{t.Fatal("duplicate hash")};r,e:=s.ClientByTokenHash("hash");if e!=nil||r.ID!=c.ID||*r.ExpiresAt!=expiry||r.Meta!="{}"{t.Fatal(r,e)};if e=s.TouchClient(c.ID,"ip1");e!=nil{t.Fatal(e)};now+=30;s.TouchClient(c.ID,"ip2");r,_=s.ClientByTokenHash("hash");if *r.LastUsedAt!=1000||r.LastUsedIP!="ip1"{t.Fatal(r)};now+=31;s.TouchClient(c.ID,"ip3");if e=s.RevokeClient(c.ID);e!=nil{t.Fatal(e)};r,_=s.ClientByTokenHash("hash");if r.RevokedAt==nil||r.LastUsedIP!="ip3"{t.Fatal(r)};if r,e=s.ClientByTokenHash("absent");e!=nil||r!=nil{t.Fatal(r,e)}}
// Behavior: app redeploys preserve overlays, no-promote leaves main unchanged, and data after rollback builds on main.
func TestVersionsSnapshotCurrentLayersAndRollback(t *testing.T){s:=openTest(t);a:=appTest(t,s);v1:=deploy(t,s,a,"old",true);v2,e:=s.CommitDataVersion(a.ID,"data/",layer("data/a.json","data"),"","c_b");if e!=nil{t.Fatal(e)};v3:=deploy(t,s,a,"new",false);r,_:=s.ResolveVersion(a.ID,0);if r.Number!=v2.Number{t.Fatal(r)};r,_=s.ResolveVersion(a.ID,v3.Number);if len(r.Overlays)!=1||r.Overlays[0].PushedBy!="c_b"{t.Fatal(r)};if e=s.Promote(a.ID,v1.Number);e!=nil{t.Fatal(e)};v4,e:=s.CommitDataVersion(a.ID,"other/",layer("other/x","x"),"","c_c");if e!=nil||v4.BaseLayer!=v1.BaseLayer||len(v4.Overlays)!=1{t.Fatal(v4,e)};v5,e:=s.CommitDataVersion(a.ID,"other/",nil,"","c_c");if e!=nil||len(v5.Overlays)!=0{t.Fatal(v5,e)};if _,e=s.CommitDataVersion(a.ID,"other/",nil,"","c_c");!errors.Is(e,ErrOverlayNotFound){t.Fatal(e)};if e=s.Promote(a.ID,999);!errors.Is(e,ErrVersionNotFound){t.Fatal(e)};r,_=s.ResolveVersion(a.ID,v1.Number);f,e:=s.LookupFile(r.BaseLayer,"index.html");if e!=nil||f.Blob!="old"{t.Fatal(f,e)};if f,e=s.LookupFile(r.BaseLayer,"absent");e!=nil||f!=nil{t.Fatal(f,e)}}
// Behavior: empty apps accept data with an empty base; nested overlays are rejected, while equal directories replace a layer.
func TestDataOnEmptyAppsAndOverlayValidation(t *testing.T){s:=openTest(t);a:=appTest(t,s);v,e:=s.CommitDataVersion(a.ID,"data",layer("data/x","x"),"","c_a");if e!=nil||v.Number!=1||len(v.Overlays)!=1{t.Fatal(v,e)};if f,e:=s.LookupFile(v.BaseLayer,"index.html");e!=nil||f!=nil{t.Fatal(f,e)};for _,d:=range []string{"data/sub/","./","/","../x",".well-known/","x/.hidden/","x//y","x\\y"}{if _,e=s.CommitDataVersion(a.ID,d,layer("x","x"),"","c_a");e==nil{t.Fatal(d)}};if _,e=s.CommitDataVersion(a.ID,"data/",layer("data/y","y"),"","c_b");e!=nil{t.Fatal(e)};a2,e:=s.CreateApp("other","","private","c_b");if e!=nil{t.Fatal(e)};s.CommitDataVersion(a2.ID,"data/sub/",layer("data/sub/x","x"),"","c_a");if _,e=s.CommitDataVersion(a2.ID,"data/",layer("data/x","x"),"","c_a");!errors.Is(e,ErrOverlayConflict){t.Fatal(e)}}
// Behavior: failed layer inserts roll back the layer, version number, title and main pointer together.
func TestFailedCommitsRollBackEveryWrite(t *testing.T){s:=openTest(t);a:=appTest(t,s);bad:=&Layer{Files:[]File{{Path:"x",Blob:"x"},{Path:"x",Blob:"y"}}};if _,e:=s.CommitAppVersion(a.ID,bad,bundle.Manifest{Title:"bad"},"","c_a",true);e==nil{t.Fatal("duplicate files")};var n int;s.db.QueryRow("SELECT count(*) FROM layers").Scan(&n);if n!=0{t.Fatal(n)};r,_:=s.AppBySlug(a.Slug);if r.NextVersion!=1||r.CurrentVersion!=nil||r.Title!="Title"{t.Fatal(r)};if v:=deploy(t,s,a,"good",true);v.Number!=1{t.Fatal(v)}}
// Behavior: concurrent publishers allocate unique sequential immutable versions.
func TestConcurrentPublishesAllocateSequentialVersions(t *testing.T){s:=openTest(t);a:=appTest(t,s);var wg sync.WaitGroup;for i:=0;i<12;i++{wg.Add(1);go func(){defer wg.Done();if _,e:=s.CommitAppVersion(a.ID,layer("index.html","x"),bundle.Manifest{},"","c_a",true);e!=nil{t.Error(e)}}()};wg.Wait();vs,e:=s.Versions(a.ID);if e!=nil||len(vs)!=12{t.Fatal(len(vs),e)};for i,v:=range vs{if v.Number!=12-i{t.Fatal(v.Number)}}}
// Behavior: trash reserves slugs and rejects writes until purge cascades versions; GC retains app versions and pinned main.
func TestGarbageCollectionPreservesReferencesAndReleasesPurgedSlugs(t *testing.T){s:=openTest(t);a:=appTest(t,s);v:=deploy(t,s,a,"base",true);var pinned int;for i:=0;i<25;i++{v,e:=s.CommitDataVersion(a.ID,"data/",layer("data/x",fmt.Sprint(i)),"","c_a");if e!=nil{t.Fatal(e)};if i==0{pinned=v.Number}};s.Promote(a.ID,pinned);if e:=s.PruneDataVersions(20);e!=nil{t.Fatal(e)};vs,_:=s.Versions(a.ID);if len(vs)!=22{t.Fatal(len(vs))};if _,e:=s.ResolveVersion(a.ID,v.Number);e!=nil{t.Fatal(e)};orphans,e:=s.OrphanLayers();if e!=nil||len(orphans)!=4{t.Fatal(len(orphans),e)};for _,l:=range orphans{if e=s.DeleteOrphanLayer(l.ID);e!=nil{t.Fatal(e)}};refs,e:=s.ReferencedBlobs();if e!=nil||!refs["base"]||refs["1"]{t.Fatal(refs,e)};if e=s.DeleteOrphanLayer(v.BaseLayer);e!=nil{t.Fatal(e)};if e=s.SoftDeleteApp(a.ID);e!=nil{t.Fatal(e)};if _,e=s.CreateApp(a.Slug,"","public","c_b");e==nil{t.Fatal("reserved")};apps,_:=s.ListApps();if len(apps)!=0{t.Fatal(apps)};if _,e=s.ResolveVersion(a.ID,0);!errors.Is(e,ErrAppNotFound){t.Fatal(e)};if _,e=s.CommitAppVersion(a.ID,layer("x","x"),bundle.Manifest{},"","c_a",true);!errors.Is(e,ErrAppNotFound){t.Fatal(e)};if e=s.PurgeTrash(time.Now().Add(-time.Hour));e!=nil{t.Fatal(e)};r,_:=s.AppBySlug(a.Slug);if r==nil{t.Fatal("purged early")};s.PurgeTrash(time.Now().Add(time.Hour));r,_=s.AppBySlug(a.Slug);if r!=nil{t.Fatal(r)};if _,e=s.CreateApp(a.Slug,"","private","c_b");e!=nil{t.Fatal(e)}}
// Behavior: events persist actor, action, app, timestamp and JSON detail.
func TestEventsRecordAuditDetails(t *testing.T){s:=openTest(t);e:=s.AddEvent("c_a","app.created","a_1",`{"slug":"report"}`);if e!=nil{t.Fatal(e)};var actor,action,detail string;var at int64;if e=s.db.QueryRow("SELECT actor,action,detail,at FROM events").Scan(&actor,&action,&detail,&at);e!=nil||actor!="c_a"||action!="app.created"||detail!=`{"slug":"report"}`||at==0{t.Fatal(actor,action,detail,at,e)}}
// Behavior: an overlay hides the entire base directory even if a requested file is absent there.
func TestOverlayLookupsNeverFallBackToBase(t *testing.T){
 s:=openTest(t);a:=appTest(t,s)
 _,e:=s.CommitAppVersion(a.ID,&Layer{Files:[]File{{Path:"index.html",Blob:"base",Size:2},{Path:"data/old.json",Blob:"old",Size:3}}},bundle.Manifest{Data:map[string]string{"data/":"spec"}},"","c_a",true)
 if e!=nil{t.Fatal(e)}
 v,e:=s.CommitDataVersion(a.ID,"data/",layer("data/new.json","new"),"","c_b");if e!=nil{t.Fatal(e)}
 if f,e:=s.LookupVersionFile(v,"data/old.json");e!=nil||f!=nil{t.Fatal(f,e)}
 if f,e:=s.LookupVersionFile(v,"data/new.json");e!=nil||f.Blob!="new"{t.Fatal(f,e)}
 if f,e:=s.LookupVersionFile(v,"index.html");e!=nil||f.Blob!="base"{t.Fatal(f,e)}
 v,e=s.CommitDataVersion(a.ID,"data/",nil,"","c_b");if e!=nil{t.Fatal(e)}
 if f,e:=s.LookupVersionFile(v,"data/old.json");e!=nil||f.Blob!="old"{t.Fatal(f,e)}
 if v.Manifest.Data["data/"]!="spec"{t.Fatal(v.Manifest)}
}
// Behavior: a database created by a future schema is rejected rather than silently downgraded.
func TestFutureSchemasAreRejected(t *testing.T){p:=filepath.Join(t.TempDir(),"db");s,e:=Open(p);if e!=nil{t.Fatal(e)};s.db.Exec("PRAGMA user_version=2");s.Close();if s,e=Open(p);e==nil{s.Close();t.Fatal("accepted future database")}}
// Behavior: independent database connections serialize publishers without duplicate version numbers or lost main updates.
func TestIndependentStoresSerializePublishers(t *testing.T){
 p:=filepath.Join(t.TempDir(),"db");a,e:=Open(p);if e!=nil{t.Fatal(e)};defer a.Close()
 b,e:=Open(p);if e!=nil{t.Fatal(e)};defer b.Close();app:=appTest(t,a)
 var wg sync.WaitGroup
 for _,s:=range []*Store{a,b}{wg.Add(1);go func(s *Store){defer wg.Done();for i:=0;i<5;i++{if _,e:=s.CommitAppVersion(app.ID,layer("index.html","content"),bundle.Manifest{},"","c_a",true);e!=nil{t.Error(e);return}}}(s)}
 wg.Wait();versions,e:=a.Versions(app.ID);if e!=nil||len(versions)!=10{t.Fatal(len(versions),e)}
 current,e:=b.ResolveVersion(app.ID,0);if e!=nil||current.Number!=10{t.Fatal(current,e)}
}
