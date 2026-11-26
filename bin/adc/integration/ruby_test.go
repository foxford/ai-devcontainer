package integration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Keep this fixture explicit: the runtime supplies asdf and build dependencies,
// while each project chooses and installs its own Ruby version.
const smokeRubyVersion = "3.4.13"

func checkRubyToolchain(t *testing.T, docker func(...string) string, container, projectA, projectB string) func() {
	t.Helper()
	write := func(project, name, contents string) {
		t.Helper()
		path := filepath.Join(project, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	execProject := func(project string, args ...string) string {
		t.Helper()
		return docker(append([]string{"exec", "--user", "node", "--workdir", project, container}, args...)...)
	}
	assertContains := func(value, want string) {
		t.Helper()
		if !strings.Contains(value, want) {
			t.Fatalf("expected %q in:\n%s", want, value)
		}
	}
	write(projectA, ".tool-versions", "ruby "+smokeRubyVersion+"\n")
	// A second project must not silently inherit the first project's Ruby.
	// This checks resolution and isolation, not two installed Ruby versions.
	write(projectB, ".tool-versions", "ruby 3.3.0\n")
	execProject(projectA, "test", "!", "-e", "/home/node/.tool-versions")
	execProject(projectA, "test", "!", "-e", "/home/node/.asdf/installs/ruby")
	t.Log("installing the fixture project's Ruby with asdf")
	execProject(projectA, "env", "MAKE_OPTS=-j2", "RUBY_CONFIGURE_OPTS=--disable-install-doc", "asdf", "install", "ruby")

	for name, contents := range rubyNativeFixture {
		write(projectA, filepath.Join("adc-ruby-smoke", name), contents)
	}
	fixture := filepath.Join(projectA, "adc-ruby-smoke")
	// Build a real local gem so missing headers, link libraries, Ruby development
	// files or compiler support fail without private repositories or gem downloads.
	execProject(fixture, "gem", "build", "adc_native_smoke.gemspec")
	execProject(fixture, "gem", "install", "--local", "--no-document", "adc_native_smoke-0.0.1.gem")
	execProject(fixture, "bundle", "install", "--local")
	execProject(fixture, "cmake", "-S", "native", "-B", "native/build")
	execProject(fixture, "cmake", "--build", "native/build", "--parallel", "2")
	assertContains(execProject(fixture, "./native/build/adc_native_smoke"), "ADC_CMAKE_NATIVE_OK")
	execProject(projectA, "sh", "-ec", `printf 'ADC_RUBY_PLUGIN_PERSIST\n' > "$ASDF_DATA_DIR/plugins/ruby/adc-smoke-preserve"`)

	checkShells := func() {
		t.Helper()
		for _, shell := range []struct{ name, flags string }{{"bash", "-lc"}, {"zsh", "-lic"}} {
			check := fmt.Sprintf(`test "$(command -v asdf)" = /usr/local/bin/asdf
test "$(command -v ruby)" = "$ASDF_DATA_DIR/shims/ruby"
test "$(command -v bundle)" = "$ASDF_DATA_DIR/shims/bundle"
ruby -e 'abort RUBY_VERSION unless RUBY_VERSION == %q'
bundle check`, smokeRubyVersion)
			// Change directories in the same shell: cached command lookup must
			// still honor each project's selection when invoking a shared shim.
			script := `set -e
cd "$1"
` + check + `
cd "$2"
if ruby -e 'exit 0'; then echo ADC_UNEXPECTED_RUBY_FALLBACK; exit 1; fi
cd "$1"
` + check + `
printf 'ADC_PROJECT_RUBY_RESOLUTION_OK\n'
`
			assertContains(execProject(projectA, shell.name, shell.flags, script, "adc-ruby-smoke", fixture, projectB), "ADC_PROJECT_RUBY_RESOLUTION_OK")
		}
	}
	checkShells()
	state := func() string {
		t.Helper()
		return strings.TrimSpace(execProject(fixture, "bundle", "exec", "ruby", "-rjson", "-rrbconfig", "-rdigest", "-radc_native_smoke", "-e", `pg, clang = ADCNativeSmoke.dependencies; abort "native libraries unavailable" unless pg > 0 && !clang.empty?; puts JSON.generate(version: RUBY_VERSION, executable: RbConfig.ruby, executable_sha256: Digest::SHA256.file(RbConfig.ruby).hexdigest, gem_home: Gem.dir, dependencies: [pg, clang])`))
	}
	before := state()
	assertContains(before, `"version":"`+smokeRubyVersion+`"`)
	assertContains(before, "/home/node/.asdf/installs/ruby/"+smokeRubyVersion)
	return func() {
		t.Helper()
		checkShells()
		if after := state(); after != before {
			t.Fatalf("Ruby executable, gem home or native gem changed after recreation:\nbefore: %s\nafter: %s", before, after)
		}
		assertContains(execProject(projectA, "cat", "/home/node/.asdf/plugins/ruby/adc-smoke-preserve"), "ADC_RUBY_PLUGIN_PERSIST")
		assertContains(execProject(fixture, "./native/build/adc_native_smoke"), "ADC_CMAKE_NATIVE_OK")
	}
}

var rubyNativeFixture = map[string]string{
	"Gemfile": `source "https://rubygems.org"
gem "adc_native_smoke", "= 0.0.1"
`,
	"adc_native_smoke.gemspec": `Gem::Specification.new do |spec|
  spec.name = "adc_native_smoke"
  spec.version = "0.0.1"
  spec.summary = "ADC native dependency integration fixture"
  spec.authors = ["ADC"]
  spec.files = Dir["lib/**/*.rb", "ext/**/*.{rb,c}"]
  spec.require_paths = ["lib"]
  spec.extensions = ["ext/adc_native_smoke/extconf.rb"]
end
`,
	"lib/adc_native_smoke.rb": `require "adc_native_smoke/adc_native_smoke"
`,
	"ext/adc_native_smoke/extconf.rb": `require "mkmf"
clang_include = Dir["/usr/lib/llvm-*/include"].find { |path| File.file?(File.join(path, "clang-c/Index.h")) }
abort "missing clang headers" unless clang_include
dir_config("clang", clang_include, File.join(File.dirname(clang_include), "lib"))
abort "missing libpq pkg-config metadata" unless pkg_config("libpq")
abort "missing libpq headers" unless have_header("libpq-fe.h")
abort "missing clang headers" unless have_header("clang-c/Index.h")
abort "missing libpq library" unless have_library("pq", "PQlibVersion")
abort "missing clang library" unless have_library("clang", "clang_createIndex")
create_makefile("adc_native_smoke/adc_native_smoke")
`,
	"ext/adc_native_smoke/adc_native_smoke.c": `#include <ruby.h>
#include <libpq-fe.h>
#include <clang-c/Index.h>

static VALUE dependencies(VALUE self) {
  CXString clang_version = clang_getClangVersion();
  VALUE result = rb_ary_new_from_args(2, INT2NUM(PQlibVersion()), rb_str_new_cstr(clang_getCString(clang_version)));
  clang_disposeString(clang_version);
  return result;
}

void Init_adc_native_smoke(void) {
  VALUE module = rb_define_module("ADCNativeSmoke");
  rb_define_singleton_method(module, "dependencies", dependencies, 0);
}
`,
	"native/CMakeLists.txt": `cmake_minimum_required(VERSION 3.18)
project(adc_native_smoke C)
find_package(PostgreSQL REQUIRED)
file(GLOB llvm_dirs LIST_DIRECTORIES true /usr/lib/llvm-*)
find_path(CLANG_INCLUDE_DIR clang-c/Index.h HINTS ${llvm_dirs} PATH_SUFFIXES include REQUIRED)
find_library(CLANG_LIBRARY NAMES clang HINTS ${llvm_dirs} PATH_SUFFIXES lib REQUIRED)
add_executable(adc_native_smoke check.c)
target_include_directories(adc_native_smoke PRIVATE ${CLANG_INCLUDE_DIR})
target_link_libraries(adc_native_smoke PRIVATE PostgreSQL::PostgreSQL ${CLANG_LIBRARY})
`,
	"native/check.c": `#include <stdio.h>
#include <libpq-fe.h>
#include <clang-c/Index.h>

int main(void) {
  CXString version = clang_getClangVersion();
  const char *description = clang_getCString(version);
  int available = PQlibVersion() > 0 && description != NULL && description[0] != '\0';
  clang_disposeString(version);
  if (!available) return 1;
  puts("ADC_CMAKE_NATIVE_OK");
  return 0;
}
`,
}
