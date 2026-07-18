package rules

import (
	"strings"
	"testing"
)

func TestJavaLayerViolation(t *testing.T) {
	cfg := `
layers:
  domain:
    path: "src/main/java/com/example/domain/**"
  infra:
    path: "src/main/java/com/example/infra/**"
rules:
  - allow: infra -> domain
  - deny: domain -> "*"
    reason: keep the domain pure
`
	vs := project(t, cfg, map[string]string{
		"src/main/java/com/example/domain/User.java": `package com.example.domain;

import com.example.infra.UserDao;

public class User {}
`,
		"src/main/java/com/example/infra/UserDao.java": `package com.example.infra;

import com.example.domain.User;

public class UserDao {}
`,
	})
	if len(vs) != 1 {
		t.Fatalf("violations = %d, want 1: %+v", len(vs), vs)
	}
	if !strings.HasSuffix(vs[0].File, "domain/User.java") || vs[0].Line != 3 {
		t.Errorf("wrong location: %+v", vs[0])
	}
}

func TestMavenAndGradleDependencyGate(t *testing.T) {
	cfg := `
layers:
  app:
    path: "src/**"
dependencies:
  deny: ["org.projectlombok:*", "com.google.guava:guava"]
  reason: curated deps only
`
	vs := project(t, cfg, map[string]string{
		"src/main/java/App.java": "public class App {}\n",
		"pom.xml": `<?xml version="1.0"?>
<project>
  <dependencies>
    <dependency>
      <groupId>org.projectlombok</groupId>
      <artifactId>lombok</artifactId>
    </dependency>
    <dependency>
      <groupId>org.springframework</groupId>
      <artifactId>spring-core</artifactId>
    </dependency>
  </dependencies>
</project>
`,
		"build.gradle": `dependencies {
    implementation 'com.google.guava:guava:33.0.0-jre'
    testImplementation "org.junit.jupiter:junit-jupiter:5.10.0"
}
`,
	})
	var got []string
	for _, v := range vs {
		got = append(got, v.File+"|"+v.Detail)
	}
	joined := strings.Join(got, "; ")
	if len(vs) != 2 {
		t.Fatalf("violations = %d, want 2 (lombok, guava): %v", len(vs), joined)
	}
	if !strings.Contains(joined, "org.projectlombok:lombok") || !strings.Contains(joined, "com.google.guava:guava") {
		t.Errorf("wrong deps flagged: %v", joined)
	}
}
