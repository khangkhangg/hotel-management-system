package siteprofile_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"hotelpms/api/internal/siteprofile"
	"hotelpms/api/internal/testkit"
	"hotelpms/api/internal/translate"
)

const png = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

type hotel struct {
	w                *testkit.World
	prop             uuid.UUID
	owner, mgr, desk *testkit.Person
}

func newHotel(t *testing.T) *hotel { return newHotelWith(t, nil) }

func newHotelWith(t *testing.T, tr translate.Translator) *hotel {
	w := testkit.New(t, siteprofile.MountWith(tr))
	h := &hotel{w: w, prop: w.A1}
	h.owner = w.Member(w.OrgA, nil, "owner")
	h.mgr = w.Member(w.OrgA, &w.A1, "manager")
	h.desk = w.Member(w.OrgA, &w.A1, "front_desk")
	return h
}

func (h *hotel) get(t *testing.T) (out struct {
	Slug      string
	Color     string
	Logo      string
	Texts     map[string]map[string]string
	Auto      map[string][]string
	Amenities []string
	Catalog   []string
	Photos    []struct{ ID, Caption string }
}) {
	t.Helper()
	testkit.Must(t, json.Unmarshal([]byte(h.owner.Do("GET", "/api/site", &h.prop, "").Want(t, 200).Body), &out))
	return out
}

func upload(kind, caption, data string) string {
	return fmt.Sprintf(`{"kind":%q,"caption":%q,"data":%q}`, kind, caption, data)
}

func TestOnlyTheOwnerChangesTheSiteAndAnEmptySiteReadsAsEmpty(t *testing.T) {
	h := newHotel(t)
	for _, p := range []*testkit.Person{h.mgr, h.desk} {
		p.Do("GET", "/api/site", &h.prop, "").Want(t, 403)
		p.Do("PUT", "/api/site/brand", &h.prop, `{"color":"#1F5F4F"}`).Want(t, 403)
	}
	s := h.get(t)
	if s.Color != "" || s.Logo != "" || len(s.Photos) != 0 || len(s.Amenities) != 0 || len(s.Catalog) < 10 || s.Slug == "" {
		t.Fatalf("%+v", s)
	}
}

func TestTheBrandColourMustBeAHexValueThatCarriesWhiteText(t *testing.T) {
	h := newHotel(t)
	h.owner.Do("PUT", "/api/site/brand", &h.prop, `{"color":"#1F5F4F"}`).Want(t, 204)
	if c := h.get(t).Color; c != "#1F5F4F" {
		t.Fatalf("%q", c)
	}
	h.owner.Do("PUT", "/api/site/brand", &h.prop, `{"color":"#FFE680"}`).Want(t, 400, "color_too_light")
	for _, bad := range []string{`"red"`, `"#12"`, `"#GGGGGG"`, `"1F5F4F"`, `""`} {
		h.owner.Do("PUT", "/api/site/brand", &h.prop, `{"color":`+bad+`}`).Want(t, 400)
	}
	if c := h.get(t).Color; c != "#1F5F4F" {
		t.Fatalf("a refused colour changed the brand: %q", c)
	}
}

func TestTextIsKeptPerLanguageAndFieldAndAnEmptyTextRemovesIt(t *testing.T) {
	h := newHotel(t)
	h.owner.Do("PUT", "/api/site/texts/en/description", &h.prop, `{"body":"A quiet hotel by the sea."}`).Want(t, 204)
	h.owner.Do("PUT", "/api/site/texts/vi/description", &h.prop, `{"body":"x"}`).Want(t, 400) // not one of the ten
	h.owner.Do("PUT", "/api/site/texts/zh-Hans/tagline", &h.prop, `{"body":"海边的安静酒店"}`).Want(t, 204)
	h.owner.Do("PUT", "/api/site/texts/en/menu", &h.prop, `{"body":"x"}`).Want(t, 400)
	h.owner.Do("PUT", "/api/site/texts/en/policies", &h.prop, `{"body":"`+strings.Repeat("a", 2001)+`"}`).Want(t, 400)
	h.owner.Do("PUT", "/api/site/texts/en/tagline", &h.prop, `{"body":"`+strings.Repeat("a", 121)+`"}`).Want(t, 400)
	s := h.get(t)
	if s.Texts["en"]["description"] != "A quiet hotel by the sea." || s.Texts["zh-Hans"]["tagline"] != "海边的安静酒店" {
		t.Fatalf("%+v", s.Texts)
	}
	h.owner.Do("PUT", "/api/site/texts/en/description", &h.prop, `{"body":"  "}`).Want(t, 204)
	if _, there := h.get(t).Texts["en"]["description"]; there {
		t.Fatal("an empty text stayed")
	}
}

func TestAmenitiesComeFromTheCatalogAndTheListIsReplacedAsAWhole(t *testing.T) {
	h := newHotel(t)
	h.owner.Do("PUT", "/api/site/amenities", &h.prop, `{"codes":["wifi","pool","wifi"]}`).Want(t, 204)
	if a := h.get(t).Amenities; len(a) != 2 {
		t.Fatalf("%v", a)
	}
	h.owner.Do("PUT", "/api/site/amenities", &h.prop, `{"codes":["wifi","helipad"]}`).Want(t, 400)
	if a := h.get(t).Amenities; len(a) != 2 {
		t.Fatalf("a refused list changed the amenities: %v", a)
	}
	h.owner.Do("PUT", "/api/site/amenities", &h.prop, `{"codes":["spa"]}`).Want(t, 204)
	if a := h.get(t).Amenities; len(a) != 1 || a[0] != "spa" {
		t.Fatalf("%v", a)
	}
}

func TestPhotosMustBeSmallRealImagesAndTheLogoIsOne(t *testing.T) {
	h := newHotel(t)
	h.owner.Do("POST", "/api/site/photos", &h.prop, upload("gallery", "Pool", png)).Want(t, 201)
	h.owner.Do("POST", "/api/site/photos", &h.prop, upload("gallery", "x", "aGVsbG8gd29ybGQ=")).Want(t, 400, "not_an_image") // text
	h.owner.Do("POST", "/api/site/photos", &h.prop, upload("gallery", "x", "PHN2Zz48L3N2Zz4=")).Want(t, 400, "not_an_image") // svg
	h.owner.Do("POST", "/api/site/photos", &h.prop, upload("gallery", "x", "%%%")).Want(t, 400)
	h.owner.Do("POST", "/api/site/photos", &h.prop, upload("banner", "x", png)).Want(t, 400)
	big := "iVBORw0KGgo" + strings.Repeat("A", 3*1024*1024)
	h.owner.Do("POST", "/api/site/photos", &h.prop, upload("gallery", "x", big)).Want(t, 413)
	h.owner.Do("POST", "/api/site/photos", &h.prop, upload("logo", "", png)).Want(t, 201)
	h.owner.Do("POST", "/api/site/photos", &h.prop, upload("logo", "", png)).Want(t, 201)
	s := h.get(t)
	if len(s.Photos) != 1 || s.Photos[0].Caption != "Pool" || s.Logo == "" {
		t.Fatalf("%+v", s)
	}
	for i := 0; i < 11; i++ {
		h.owner.Do("POST", "/api/site/photos", &h.prop, upload("gallery", "", png)).Want(t, 201)
	}
	h.owner.Do("POST", "/api/site/photos", &h.prop, upload("gallery", "", png)).Want(t, 409, "too_many_photos")
	h.owner.Do("DELETE", "/api/site/photos/"+s.Photos[0].ID, &h.prop, "").Want(t, 204)
	h.owner.Do("DELETE", "/api/site/photos/"+s.Photos[0].ID, &h.prop, "").Want(t, 404)
	h.owner.Do("DELETE", "/api/site/photos/"+s.Logo, &h.prop, "").Want(t, 204)
	if got := h.get(t); got.Logo != "" || len(got.Photos) != 11 {
		t.Fatalf("%+v", got)
	}
}

func TestAnotherPropertyCannotDeleteThisPropertysPhoto(t *testing.T) {
	h := newHotel(t)
	id := func() string {
		var b struct{ ID string }
		testkit.Must(t, json.Unmarshal([]byte(h.owner.Do("POST", "/api/site/photos", &h.prop, upload("gallery", "", png)).Want(t, 201).Body), &b))
		return b.ID
	}()
	other := h.w.A2
	h.owner.Do("DELETE", "/api/site/photos/"+id, &other, "").Want(t, 404)
	if len(h.get(t).Photos) != 1 {
		t.Fatal("the photo was deleted from another property")
	}
}

func TestTheOwnerFillsTheOtherLanguagesFromOneAndEveryTranslationIsADraftThatEditingTakesOver(t *testing.T) {
	fake := translate.NewFake()
	h := newHotelWith(t, fake)
	h.owner.Do("PUT", "/api/site/texts/en/description", &h.prop, `{"body":"A quiet hotel by the sea."}`).Want(t, 204)
	h.owner.Do("PUT", "/api/site/texts/en/tagline", &h.prop, `{"body":"Rest by the sea"}`).Want(t, 204)
	h.owner.Do("PUT", "/api/site/texts/fr/description", &h.prop, `{"body":"Notre texte à nous"}`).Want(t, 204) // written by hand, so never replaced
	h.mgr.Do("POST", "/api/site/translate", &h.prop, `{"from":"en"}`).Want(t, 403)
	h.owner.Do("POST", "/api/site/translate", &h.prop, `{"from":"en"}`).Want(t, 200, `"created":17`) // nine languages and two texts, less the one already written
	s := h.get(t)
	if s.Texts["es"]["description"] != "[es] A quiet hotel by the sea." || s.Texts["zh-Hans"]["tagline"] != "[zh-Hans] Rest by the sea" {
		t.Fatalf("%+v", s.Texts)
	}
	if s.Texts["fr"]["description"] != "Notre texte à nous" || s.Texts["fr"]["tagline"] != "[fr] Rest by the sea" {
		t.Fatalf("a hand written text was replaced: %+v", s.Texts["fr"])
	}
	if len(s.Auto["es"]) != 2 || len(s.Auto["fr"]) != 1 || s.Auto["fr"][0] != "tagline" || len(s.Auto["en"]) != 0 {
		t.Fatalf("which texts are machine drafts: %+v", s.Auto)
	}
	calls := fake.Calls
	h.owner.Do("POST", "/api/site/translate", &h.prop, `{"from":"en"}`).Want(t, 200, `"created":0`)
	if fake.Calls != calls {
		t.Fatal("the service was asked again for texts that are already there")
	}
	// A person who edits a draft takes it over, and a refresh then leaves it alone.
	h.owner.Do("PUT", "/api/site/texts/es/description", &h.prop, `{"body":"Un hotel tranquilo junto al mar."}`).Want(t, 204)
	h.owner.Do("PUT", "/api/site/texts/en/description", &h.prop, `{"body":"A calm hotel by the sea."}`).Want(t, 204)
	h.owner.Do("POST", "/api/site/translate", &h.prop, `{"from":"en","refresh":true}`).Want(t, 200)
	s = h.get(t)
	if s.Texts["es"]["description"] != "Un hotel tranquilo junto al mar." || s.Texts["de"] != nil {
		t.Fatalf("%+v", s.Texts["es"])
	}
	if s.Texts["pt"]["description"] != "[pt] A calm hotel by the sea." {
		t.Fatalf("a draft was not refreshed from the new source: %q", s.Texts["pt"]["description"])
	}
}

func TestTranslationNeedsATextAndAServiceAndASingleFailureSavesNothing(t *testing.T) {
	h := newHotel(t) // no service configured
	h.owner.Do("PUT", "/api/site/texts/en/description", &h.prop, `{"body":"A quiet hotel."}`).Want(t, 204)
	h.owner.Do("POST", "/api/site/translate", &h.prop, `{"from":"en"}`).Want(t, 503, "translation_off")

	fake := translate.NewFake()
	h = newHotelWith(t, fake)
	h.owner.Do("POST", "/api/site/translate", &h.prop, `{"from":"en"}`).Want(t, 400, "nothing_to_translate")
	h.owner.Do("POST", "/api/site/translate", &h.prop, `{"from":"vi"}`).Want(t, 400)
	h.owner.Do("POST", "/api/site/translate", &h.prop, `{}`).Want(t, 400)
	h.owner.Do("PUT", "/api/site/texts/en/description", &h.prop, `{"body":"A quiet hotel."}`).Want(t, 204)
	fake.Fail = true
	h.owner.Do("POST", "/api/site/translate", &h.prop, `{"from":"en"}`).Want(t, 502, "translation_failed")
	if s := h.get(t); len(s.Texts) != 1 {
		t.Fatalf("a failed translation left texts behind: %+v", s.Texts)
	}
}
