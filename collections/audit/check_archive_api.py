#!/usr/bin/env python3
"""Compile an external consumer of the public custom archive contract."""
import pathlib
import shutil
import subprocess
import tempfile

root = pathlib.Path(__file__).resolve().parents[2]
work = pathlib.Path(tempfile.mkdtemp(prefix="collections-consumer-audit-", dir="/private/tmp"))
try:
    (work / "go.mod").write_text(
        "module example.com/collections-consumer\n\ngo 1.26.1\n\n"
        "require github.com/elum2b/services v0.0.0\n"
        "replace github.com/elum2b/services => " + str(root) + "\n"
    )
    source = work / "consumer.go"
    source.write_text('package consumer\nimport svc "github.com/elum2b/services/collections"\nvar Options=svc.Options{}\n')
    baseline = subprocess.run(["go", "test", "-mod=mod", "./..."], cwd=work, capture_output=True, text=True)
    if baseline.returncode:
        raise SystemExit("Baseline external import failed:\n" + baseline.stdout + baseline.stderr)
    print("External root import: compiled")
    source.write_text('''package consumer
import (
    "context"
    "io"
    svc "github.com/elum2b/services/collections"
)
type CustomArchive struct{}
func(CustomArchive) Store(context.Context,svc.ArchiveObject,io.Reader)(string,error){return "",nil}
func(CustomArchive) Open(context.Context,string)(io.ReadCloser,error){return nil,nil}
func(CustomArchive) Delete(context.Context,string)error{return nil}
func(CustomArchive) List(context.Context)([]svc.ArchiveInfo,error){return nil,nil}
var _ svc.ArchiveLister=CustomArchive{}
var Options=svc.Options{Archive:CustomArchive{}}
''')
    result = subprocess.run(["go", "test", "-mod=mod", "./..."], cwd=work, capture_output=True, text=True)
    print(result.stdout + result.stderr)
    if result.returncode:
        raise SystemExit("Public custom Archive consumer failed")
    print("PASS: external Archive and ArchiveLister implementations compile")
finally:
    shutil.rmtree(work)
