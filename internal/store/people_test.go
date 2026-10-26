package store

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPersonCreateResolveAndUniqueness(t *testing.T) {
	w := mustInit(t)
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, TodoLocation())

	p, err := w.CreatePerson(NewPerson{Name: "张三", Aliases: []string{"老张", "张哥"}, Group: "框架组", Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if p.Role != RoleMember || !p.Active {
		t.Errorf("默认角色/在职 = %s %v", p.Role, p.Active)
	}
	if len(p.GroupHistory) != 1 || p.GroupHistory[0].Group != "框架组" || p.GroupHistory[0].From != "2026-10-26" {
		t.Errorf("加入记录 = %+v", p.GroupHistory)
	}

	if got, err := w.ResolvePerson("老张"); err != nil || got.ID != p.ID {
		t.Errorf("按别名解析 = %v, %v", got, err)
	}
	if got, err := w.ResolvePerson("  zhangsan "); err == nil {
		t.Errorf("大小写不敏感应解析失败（中文拼音别名不存在），得到 %v", got)
	}

	// 冲突：在职人员姓名/别名唯一。
	if _, err := w.CreatePerson(NewPerson{Name: "张三", Now: now}); !errors.Is(err, ErrConflict) {
		t.Errorf("姓名冲突错误 = %v，期望 ErrConflict", err)
	}
	if _, err := w.CreatePerson(NewPerson{Name: "李四", Aliases: []string{" 老张 "}, Now: now}); !errors.Is(err, ErrConflict) {
		t.Errorf("别名冲突错误 = %v，期望 ErrConflict", err)
	}

	// 离职后不再参与唯一性校验。
	p2, err := w.CreatePerson(NewPerson{Name: "李四", Aliases: []string{"李哥"}, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.UpdatePerson(p2.ID, now, func(p *Person) error { p.Active = false; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := w.CreatePerson(NewPerson{Name: "李哥", Now: now}); err != nil {
		t.Errorf("离职后别名应可复用: %v", err)
	}
}

func TestPersonMoveAndMembershipAt(t *testing.T) {
	w := mustInit(t)
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, TodoLocation())

	p, err := w.CreatePerson(NewPerson{Name: "张三", Group: "旧组", ProductLines: []string{"A"}, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	got, err := w.MovePerson(p.ID, "新组", []string{"B", "C"}, "2026-11-01", now)
	if err != nil {
		t.Fatal(err)
	}
	// 生效日期在未来，当前（10-26）仍属旧组；产品线也保持旧值。
	if got.Group != "旧组" || len(got.ProductLines) != 1 || got.ProductLines[0] != "A" {
		t.Errorf("未来转组不应提前改变当前归属 = %s %v", got.Group, got.ProductLines)
	}
	if len(got.GroupHistory) != 2 {
		t.Fatalf("转组记录数 = %d", len(got.GroupHistory))
	}

	// 周一 2026-10-26 属旧组，2026-11-02 属新组。
	group, products := got.MembershipAt(time.Date(2026, 10, 26, 0, 0, 0, 0, TodoLocation()))
	if group != "旧组" || len(products) != 1 || products[0] != "A" {
		t.Errorf("10-26 所属 = %s %v", group, products)
	}
	group, products = got.MembershipAt(time.Date(2026, 11, 2, 0, 0, 0, 0, TodoLocation()))
	if group != "新组" || len(products) != 2 {
		t.Errorf("11-02 所属 = %s %v", group, products)
	}
}

func TestPersonFrontMatterRoundTrip(t *testing.T) {
	w := mustInit(t)
	now := time.Date(2026, 10, 26, 10, 0, 0, 0, TodoLocation())

	p, err := w.CreatePerson(NewPerson{
		Name: "张三", Aliases: []string{"老张"}, Group: "框架组", ProductLines: []string{"A"},
		Role: RoleLead, Now: now, Body: "备注",
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(w.personPath(p.ID))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"name: \"张三\"", "aliases:", "老张", "groupHistory:", "from: \"2026-10-26\""} {
		if !strings.Contains(string(data), want) {
			t.Errorf("文件缺少 %q:\n%s", want, data)
		}
	}

	got, err := w.GetPerson(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "张三" || got.Role != RoleLead || !got.Active || got.Body != "备注" {
		t.Errorf("往返读取 = %+v", got)
	}
}
