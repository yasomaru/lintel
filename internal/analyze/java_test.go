package analyze

import "testing"

func TestJavaImports(t *testing.T) {
	p := proj(t, Options{}, map[string]string{
		"src/main/java/com/example/domain/User.java":  "package com.example.domain;\n\npublic class User {}\n",
		"src/main/java/com/example/domain/Order.java": "package com.example.domain;\n\npublic record Order(String id) {}\n",
		"src/main/java/com/example/infra/UserDao.java": `package com.example.infra;

import com.example.domain.User;
import static com.example.domain.Order.builder;
import com.example.domain.*;
import java.util.List;

public class UserDao {}
`,
	})
	got := resolved(analyzeOne(t, p, "src/main/java/com/example/infra/UserDao.java"))
	want := map[string]string{
		"com.example.domain.User":          "src/main/java/com/example/domain/User.java",
		"com.example.domain.Order.builder": "src/main/java/com/example/domain/Order.java",
		"com.example.domain.*":             "src/main/java/com/example/domain/Order.java",
		"java.util.List":                   "",
	}
	for raw, target := range want {
		if got[raw] != target {
			t.Errorf("resolve(%q) = %q, want %q", raw, got[raw], target)
		}
	}
}

func TestJavaExports(t *testing.T) {
	p := proj(t, Options{}, map[string]string{
		"A.java": `public final class UserRepository {}
public interface Store {}
class PackagePrivate {}
public sealed record Point(int x) {}
`,
	})
	res := analyzeOne(t, p, "A.java")
	var names []string
	for _, s := range res.Exports {
		names = append(names, s.Name)
	}
	want := []string{"UserRepository", "Store", "Point"}
	if len(names) != len(want) {
		t.Fatalf("exports = %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("exports = %v, want %v", names, want)
		}
	}
}
