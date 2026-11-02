package bundle

import (
 "archive/tar"
 "bytes"
 "compress/gzip"
 "io"
 "os"
 "path/filepath"
 "strings"
 "testing"
)
type entry struct { name, body string; kind byte }
func archive(t *testing.T, entries ...entry) []byte {
 t.Helper();var b bytes.Buffer;gz:=gzip.NewWriter(&b);tw:=tar.NewWriter(gz)
 for _,e:=range entries {kind:=e.kind;if kind==0{kind=tar.TypeReg};h:=&tar.Header{Name:e.name,Size:int64(len(e.body)),Mode:0600,Typeflag:kind};if kind!=tar.TypeReg{h.Size=0};if err:=tw.WriteHeader(h);err!=nil{t.Fatal(err)};if h.Size>0{if _,err:=io.WriteString(tw,e.body);err!=nil{t.Fatal(err)}}}
 if err:=tw.Close();err!=nil{t.Fatal(err)};if err:=gz.Close();err!=nil{t.Fatal(err)};return b.Bytes()
}
func unpack(t *testing.T,b []byte,l Limits)(Summary,map[string]string,error){t.Helper();files:=map[string]string{};s,e:=Unpack(bytes.NewReader(b),l,func(p string,r io.Reader)error{v,e:=io.ReadAll(r);files[p]=string(v);return e});return s,files,e}
func write(t *testing.T,root,p,v string){t.Helper();if err:=os.MkdirAll(filepath.Dir(filepath.Join(root,p)),0700);err!=nil{t.Fatal(err)};if err:=os.WriteFile(filepath.Join(root,p),[]byte(v),0600);err!=nil{t.Fatal(err)}}
// Behavior: packing and unpacking both discard dotfiles except .well-known and never expose tinyhost.json.
func TestDotfilesAreNeverServed(t *testing.T){
 t.Run("pack",func(t *testing.T){d:=t.TempDir();for _,p:=range []string{"index.html",".env",".git/config","x/.secret",".well-known/info","node_modules/pkg/a","x/node_modules/b"}{write(t,d,p,"ok")};write(t,d,"tinyhost.json",`{"app":"abc","title":"Title","data":{"data/":"sales"},"future":1}`);if e:=os.Symlink("index.html",filepath.Join(d,"link"));e!=nil{t.Fatal(e)};p,s,e:=Pack(d);if e!=nil{t.Fatal(e)};defer os.Remove(p);f,_:=os.Open(p);defer f.Close();u,e:=Unpack(f,Limits{RequireIndex:true},func(p string,r io.Reader)error{_,e:=io.Copy(io.Discard,r);return e});if e!=nil||u.Files!=2||s.Files!=2||len(s.Warnings)!=1||u.Manifest.Title!="Title"{t.Fatal(u,s,e)}})
 t.Run("unpack",func(t *testing.T){s,f,e:=unpack(t,archive(t,entry{".env","secret",0},entry{"x/.git/config","secret",0},entry{".well-known/info","ok",0},entry{"tinyhost.json",`{"title":"Title"}`,0}),Limits{});if e!=nil||len(f)!=1||len(s.Skipped)!=2||s.Manifest.Title!="Title"{t.Fatal(s,f,e)}})
}
// Behavior: traversal, absolute, ambiguous, overlong and duplicate paths and all nonregular files reject the entire upload.
func TestUnpackRejectsTraversalAndSymlinks(t *testing.T){
 for _,p:=range []string{"../x","x/../y","/x","x\\y","x//y","",strings.Repeat("x",513)}{t.Run(p,func(t *testing.T){if _,_,e:=unpack(t,archive(t,entry{p,"x",0}),Limits{});e==nil{t.Fatal(p)}})}
 for _,kind:=range []byte{tar.TypeSymlink,tar.TypeLink,tar.TypeFifo,tar.TypeChar,tar.TypeBlock}{if _,_,e:=unpack(t,archive(t,entry{"bad","",kind}),Limits{});e==nil{t.Fatal(kind)}}
 if _,_,e:=unpack(t,archive(t,entry{"x","a",0},entry{"./x","b",0}),Limits{});e==nil{t.Fatal("duplicate")}
 if _,_,e:=unpack(t,archive(t,entry{"folder/","",tar.TypeDir},entry{"./index.html","ok",0}),Limits{RequireIndex:true});e!=nil{t.Fatal(e)}
 if _,e:=normalizePath("a\x00b");e==nil{t.Fatal("NUL")}
}
// Behavior: limits count decompressed bytes and entries even when discarded; manifest is bounded and invalid JSON fails.
func TestUnpackEnforcesLimitsAndManifest(t *testing.T){for _,c:=range []struct{entries []entry;l Limits}{
 {[]entry{{"x","1234",0}},Limits{MaxSize:3}},
 {[]entry{{".secret","1234",0}},Limits{MaxSize:3}},
 {[]entry{{"x","a",0},{"y","b",0}},Limits{MaxFiles:1}},
 {[]entry{{"tinyhost.json",strings.Repeat(" ",65537),0}},Limits{}},
 {[]entry{{"tinyhost.json","broken",0}},Limits{}},
 {[]entry{{"tinyhost.json","null",0}},Limits{}},
 {[]entry{{"tinyhost.json",`{"data":{"data/":42}}`,0}},Limits{}},
 {[]entry{{"x","a",0}},Limits{RequireIndex:true}},
 }{if _,_,e:=unpack(t,archive(t,c.entries...),c.l);e==nil{t.Fatal(c)}}
 b:=archive(t,entry{"index.html","ok",0});b[len(b)-5]^=0xff;if _,_,e:=unpack(t,b,Limits{});e==nil{t.Fatal("corrupt gzip")}
 if _,_,e:=unpack(t,[]byte("invalid"),Limits{});e==nil{t.Fatal("invalid gzip")}
 if _,e:=Unpack(bytes.NewReader(archive(t,entry{"x","a",0})),Limits{},func(string,io.Reader)error{return io.ErrClosedPipe});e==nil{t.Fatal("sink failure")}
}
// Behavior: single HTML deploys become index.html, data files retain names, and only deploy requires an index.
func TestPackSupportsSingleFilesAndDataBundles(t *testing.T){d:=t.TempDir();write(t,d,"report.html","html");p,s,e:=Pack(filepath.Join(d,"report.html"));if e!=nil||s.Files!=1{t.Fatal(s,e)};defer os.Remove(p);b,_:=os.ReadFile(p);_,f,e:=unpack(t,b,Limits{});if e!=nil||f["index.html"]!="html"{t.Fatal(f,e)};write(t,d,"sales.csv","csv");p,_,e=Pack(filepath.Join(d,"sales.csv"),Options{Data:true});if e!=nil{t.Fatal(e)};defer os.Remove(p);b,_=os.ReadFile(p);_,f,e=unpack(t,b,Limits{});if e!=nil||f["sales.csv"]!="csv"{t.Fatal(f,e)};if _,_,e=Pack(filepath.Join(d,"sales.csv"));e==nil{t.Fatal("non-html deploy")};if _,_,e=Pack(d);e==nil{t.Fatal("missing index")};if _,_,e=Pack(t.TempDir(),Options{Data:true});e==nil{t.Fatal("empty")};write(t,d,"index.html","1234");if _,_,e=Pack(d,Options{Limits:Limits{MaxSize:3}});e==nil{t.Fatal("size")};if _,_,e=Pack(d,Options{Limits:Limits{MaxFiles:1}});e==nil{t.Fatal("count")}}
// Behavior: trailing compressed data is bounded and gzip integrity is checked beyond the tar end marker.
func TestUnpackBoundsAndValidatesTrailingData(t *testing.T){
 var raw bytes.Buffer
 gz:=gzip.NewWriter(&raw)
 tw:=tar.NewWriter(gz)
 if e:=tw.Close();e!=nil{t.Fatal(e)}
 if _,e:=io.Copy(gz,strings.NewReader(strings.Repeat("x",3<<20)));e!=nil{t.Fatal(e)}
 gz.Close()
 if _,_,e:=unpack(t,raw.Bytes(),Limits{MaxSize:10,MaxFiles:1});e==nil{t.Fatal("accepted trailing decompression bomb")}
 if _,e:=normalizePath("x/");e==nil{t.Fatal("empty trailing segment")}
}
// Behavior: skipped files count towards upload limits and manifests are limited during local packing too.
func TestSkippedEntriesAndManifestsCannotBypassLimits(t *testing.T){
 if _,_,e:=unpack(t,archive(t,entry{".a","a",0},entry{".b","b",0}),Limits{MaxFiles:1});e==nil{t.Fatal("skipped count")}
 d:=t.TempDir();write(t,d,"index.html","ok");write(t,d,"tinyhost.json",strings.Repeat(" ",ManifestLimit+1))
 if _,_,e:=Pack(d);e==nil{t.Fatal("oversized manifest")}
 b:=archive(t,entry{"index.html","ab",0})
 if _,_,e:=unpack(t,b,Limits{MaxSize:2,MaxFiles:1,RequireIndex:true});e!=nil{t.Fatal(e)}
}
