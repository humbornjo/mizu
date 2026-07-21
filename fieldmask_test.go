package mizu_test

import (
	"testing"

	"github.com/humbornjo/mizu"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fieldMaskAddress struct {
	City string `json:"city"`
	Zip  string `json:"zip"`
}

type fieldMaskItem struct {
	Label string `json:"label"`
	Count int    `json:"count"`
}

type fieldMaskCustom struct {
	Nested string `json:"nested"`
}

func (fieldMaskCustom) MarshalJSON() ([]byte, error) {
	return []byte(`"custom"`), nil
}

type FieldMaskEmbedded struct {
	Embedded string `json:"embedded"`
}

type FieldMaskOptionalEmbedded struct {
	Optional string `json:"optional"`
	Sibling  string `json:"sibling"`
}

type fieldMaskEmbeddedPointer struct {
	*FieldMaskOptionalEmbedded
	Direct string `json:"direct"`
}

type fieldMaskProfile struct {
	FieldMaskEmbedded
	DisplayName       string                       `json:"displayName"`
	Age               int                          `json:"age"`
	Address           *fieldMaskAddress            `json:"address"`
	Items             []fieldMaskItem              `json:"items"`
	Fixed             [2]fieldMaskItem             `json:"fixed"`
	Attributes        map[string]fieldMaskAddress  `json:"attributes"`
	PointerAttributes map[string]*fieldMaskAddress `json:"pointerAttributes"`
	Tags              []string                     `json:"tags"`
	Custom            fieldMaskCustom              `json:"custom"`
	Secret            string                       `json:"-"`
	hidden            string
}

type FieldMaskCollisionLeft struct {
	Shared string
}

type FieldMaskCollisionRight struct {
	Shared string
}

type fieldMaskCollision struct {
	FieldMaskCollisionLeft
	FieldMaskCollisionRight
	Direct string `json:"direct"`
}

type fieldMaskUnsupportedNesting struct {
	Numbers    []int                    `json:"numbers"`
	NumericMap map[int]fieldMaskAddress `json:"numericMap"`
	Dynamic    any                      `json:"dynamic"`
}

func newFieldMaskProfile() fieldMaskProfile {
	return fieldMaskProfile{
		FieldMaskEmbedded: FieldMaskEmbedded{Embedded: "embedded"},
		DisplayName:       "name",
		Age:               42,
		Address:           &fieldMaskAddress{City: "Shanghai", Zip: "200000"},
		Items: []fieldMaskItem{
			{Label: "first", Count: 1},
			{Label: "second", Count: 2},
		},
		Fixed: [2]fieldMaskItem{
			{Label: "fixed-first", Count: 3},
			{Label: "fixed-second", Count: 4},
		},
		Attributes: map[string]fieldMaskAddress{
			"home": {City: "Hangzhou", Zip: "310000"},
			"work": {City: "Beijing", Zip: "100000"},
		},
		PointerAttributes: map[string]*fieldMaskAddress{
			"home": {City: "Ningbo", Zip: "315000"},
			"work": {City: "Shenzhen", Zip: "518000"},
		},
		Tags:   []string{"one", "two"},
		Custom: fieldMaskCustom{Nested: "inside"},
		Secret: "secret",
		hidden: "hidden",
	}
}

func TestMizu_FieldMaskIntersect(t *testing.T) {
	tests := []struct {
		name      string
		allowed   []string
		requested []string
		want      []string
	}{
		{
			name:      "exact JSON names",
			allowed:   []string{"displayName", "address.city"},
			requested: []string{"displayName", "address.city"},
			want:      []string{"address.city", "displayName"},
		},
		{
			name:      "allowed parent",
			allowed:   []string{"address"},
			requested: []string{"address.city"},
			want:      []string{"address.city"},
		},
		{
			name:      "requested parent",
			allowed:   []string{"address.city", "address.zip"},
			requested: []string{"address"},
			want:      []string{"address.city", "address.zip"},
		},
		{
			name:      "normalizes parents and duplicates",
			allowed:   []string{"address.city", "address", "address.city"},
			requested: []string{"address.city", "address.city"},
			want:      []string{"address.city"},
		},
		{
			name:    "silently omits invalid and disallowed paths",
			allowed: []string{"address", "displayName", "missing"},
			requested: []string{
				"address.city", "address..zip", "age", "missing", "DisplayName",
				"displayName.extra", "Secret", "hidden", "", ".age",
			},
			want: []string{"address.city"},
		},
		{
			name:      "containers and embedded fields",
			allowed:   []string{"items.label", "attributes.home.city", "embedded"},
			requested: []string{"items.label", "attributes.home.city", "embedded"},
			want:      []string{"attributes.home.city", "embedded", "items.label"},
		},
		{
			name:      "custom marshaler is a leaf",
			allowed:   []string{"custom", "custom.nested"},
			requested: []string{"custom", "custom.nested"},
			want:      []string{"custom"},
		},
		{
			name:      "empty intersection",
			allowed:   nil,
			requested: []string{"displayName"},
			want:      []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			allowed := append([]string(nil), tc.allowed...)
			requested := append([]string(nil), tc.requested...)
			mask := mizu.Intersect[fieldMaskProfile](allowed, requested)
			assert.Equal(t, tc.want, mask.Paths())
			assert.Equal(t, tc.allowed, allowed, "allowed input must not be modified")
			assert.Equal(t, tc.requested, requested, "requested input must not be modified")
		})
	}
}

func TestMizu_FieldMaskIntersectCollision(t *testing.T) {
	mask := mizu.Intersect[fieldMaskCollision](
		[]string{"Shared", "direct"},
		[]string{"Shared", "direct"},
	)
	assert.Equal(t, []string{"direct"}, mask.Paths())
}

func TestMizu_FieldMaskIntersectUnsupportedNesting(t *testing.T) {
	paths := []string{
		"numbers", "numbers.value",
		"numericMap", "numericMap.key.city",
		"dynamic", "dynamic.value",
	}
	mask := mizu.Intersect[fieldMaskUnsupportedNesting](paths, paths)
	assert.Equal(t, []string{"dynamic", "numbers", "numericMap"}, mask.Paths())
}

func TestMizu_FieldMaskPaths(t *testing.T) {
	mask := mizu.Intersect[fieldMaskProfile](
		[]string{"displayName", "age"},
		[]string{"displayName", "age"},
	)
	paths := mask.Paths()
	paths[0] = "changed"
	assert.Equal(t, []string{"age", "displayName"}, mask.Paths())

	var nilMask *mizu.FieldMask[fieldMaskProfile]
	assert.Nil(t, nilMask.Paths())
}

func TestMizu_FieldMaskFilter(t *testing.T) {
	profile := newFieldMaskProfile()
	paths := []string{
		"displayName",
		"address.city",
		"items.label",
		"fixed.count",
		"attributes.home.city",
		"pointerAttributes.work.zip",
		"custom",
	}
	mask := mizu.Intersect[fieldMaskProfile](paths, paths)
	require.NoError(t, mask.Filter(&profile))

	assert.Empty(t, profile.Embedded)
	assert.Equal(t, "name", profile.DisplayName)
	assert.Zero(t, profile.Age)
	require.NotNil(t, profile.Address)
	assert.Equal(t, fieldMaskAddress{City: "Shanghai"}, *profile.Address)
	assert.Equal(t, []fieldMaskItem{{Label: "first"}, {Label: "second"}}, profile.Items)
	assert.Equal(t, [2]fieldMaskItem{{Count: 3}, {Count: 4}}, profile.Fixed)
	assert.Equal(t, map[string]fieldMaskAddress{"home": {City: "Hangzhou"}}, profile.Attributes)
	assert.Equal(t, map[string]*fieldMaskAddress{"work": {Zip: "518000"}}, profile.PointerAttributes)
	assert.Nil(t, profile.Tags)
	assert.Equal(t, fieldMaskCustom{Nested: "inside"}, profile.Custom)
	assert.Equal(t, "secret", profile.Secret)
	assert.Equal(t, "hidden", profile.hidden)
}

func TestMizu_FieldMaskFilterEmpty(t *testing.T) {
	profile := newFieldMaskProfile()
	mask := mizu.Intersect[fieldMaskProfile](nil, nil)
	require.Empty(t, mask.Paths())
	require.NoError(t, mask.Filter(&profile))

	assert.Empty(t, profile.Embedded)
	assert.Empty(t, profile.DisplayName)
	assert.Zero(t, profile.Age)
	assert.Nil(t, profile.Address)
	assert.Nil(t, profile.Items)
	assert.Zero(t, profile.Fixed)
	assert.Nil(t, profile.Attributes)
	assert.Nil(t, profile.PointerAttributes)
	assert.Nil(t, profile.Tags)
	assert.Zero(t, profile.Custom)
	assert.Equal(t, "secret", profile.Secret)
	assert.Equal(t, "hidden", profile.hidden)
}

func TestMizu_FieldMaskEmbeddedPointer(t *testing.T) {
	t.Run("filter traverses promoted pointer fields", func(t *testing.T) {
		value := fieldMaskEmbeddedPointer{
			FieldMaskOptionalEmbedded: &FieldMaskOptionalEmbedded{Optional: "keep", Sibling: "clear"},
			Direct:                    "clear",
		}
		mask := mizu.Intersect[fieldMaskEmbeddedPointer](
			[]string{"optional"},
			[]string{"optional"},
		)
		require.NoError(t, mask.Filter(&value))
		assert.Equal(t, &FieldMaskOptionalEmbedded{Optional: "keep"}, value.FieldMaskOptionalEmbedded)
		assert.Empty(t, value.Direct)
	})

	t.Run("overwrite allocates promoted pointer parent", func(t *testing.T) {
		source := fieldMaskEmbeddedPointer{
			FieldMaskOptionalEmbedded: &FieldMaskOptionalEmbedded{Optional: "copied", Sibling: "ignored"},
		}
		target := fieldMaskEmbeddedPointer{}
		mask := mizu.Intersect[fieldMaskEmbeddedPointer](
			[]string{"optional"},
			[]string{"optional"},
		)
		require.NoError(t, mask.Overwrite(&source, &target))
		assert.Equal(t, &FieldMaskOptionalEmbedded{Optional: "copied"}, target.FieldMaskOptionalEmbedded)
	})

	t.Run("nil source clears promoted field without replacing parent", func(t *testing.T) {
		source := fieldMaskEmbeddedPointer{}
		target := fieldMaskEmbeddedPointer{
			FieldMaskOptionalEmbedded: &FieldMaskOptionalEmbedded{Optional: "clear", Sibling: "keep"},
		}
		mask := mizu.Intersect[fieldMaskEmbeddedPointer](
			[]string{"optional"},
			[]string{"optional"},
		)
		require.NoError(t, mask.Overwrite(&source, &target))
		assert.Equal(t, &FieldMaskOptionalEmbedded{Sibling: "keep"}, target.FieldMaskOptionalEmbedded)
	})
}

func TestMizu_FieldMaskPrune(t *testing.T) {
	profile := newFieldMaskProfile()
	paths := []string{
		"displayName",
		"address.city",
		"items.label",
		"attributes.home.city",
		"pointerAttributes.work",
		"tags",
	}
	mask := mizu.Intersect[fieldMaskProfile](paths, paths)
	require.NoError(t, mask.Prune(&profile))

	assert.Empty(t, profile.DisplayName)
	assert.Equal(t, 42, profile.Age)
	require.NotNil(t, profile.Address)
	assert.Equal(t, fieldMaskAddress{Zip: "200000"}, *profile.Address)
	assert.Equal(t, []fieldMaskItem{{Count: 1}, {Count: 2}}, profile.Items)
	assert.Equal(t, fieldMaskAddress{Zip: "310000"}, profile.Attributes["home"])
	assert.Contains(t, profile.Attributes, "work")
	assert.NotContains(t, profile.PointerAttributes, "work")
	assert.Nil(t, profile.Tags)
	assert.Equal(t, "secret", profile.Secret)
}

func TestMizu_FieldMaskPruneEmpty(t *testing.T) {
	profile := newFieldMaskProfile()
	want := newFieldMaskProfile()
	mask := mizu.Intersect[fieldMaskProfile](nil, nil)
	require.NoError(t, mask.Prune(&profile))
	assert.Equal(t, want, profile)
}

func TestMizu_FieldMaskOverwriteEmpty(t *testing.T) {
	source := fieldMaskProfile{}
	target := newFieldMaskProfile()
	want := newFieldMaskProfile()
	mask := mizu.Intersect[fieldMaskProfile](nil, nil)
	require.NoError(t, mask.Overwrite(&source, &target))
	assert.Equal(t, want, target)
}

func TestMizu_FieldMaskOverwrite(t *testing.T) {
	source := newFieldMaskProfile()
	source.DisplayName = ""
	source.Address = &fieldMaskAddress{City: "Suzhou", Zip: "source-zip"}
	source.Items = []fieldMaskItem{
		{Label: "source-first", Count: 101},
		{Label: "source-second", Count: 102},
	}
	source.Fixed = [2]fieldMaskItem{{Label: "source-fixed", Count: 30}, {Count: 40}}
	source.Attributes = map[string]fieldMaskAddress{
		"home": {City: "Chengdu", Zip: "source-home-zip"},
	}
	source.PointerAttributes = map[string]*fieldMaskAddress{
		"work": {City: "Guangzhou", Zip: "source-work-zip"},
	}
	source.Tags = []string{"source"}

	target := newFieldMaskProfile()
	target.Items = append(target.Items, fieldMaskItem{Label: "third", Count: 3})
	target.Attributes["gone"] = fieldMaskAddress{City: "remove", Zip: "remove"}
	paths := []string{
		"displayName",
		"address.city",
		"items.label",
		"fixed.count",
		"attributes.home.city",
		"attributes.gone",
		"pointerAttributes.work.city",
		"tags",
	}
	mask := mizu.Intersect[fieldMaskProfile](paths, paths)
	require.NoError(t, mask.Overwrite(&source, &target))

	assert.Empty(t, target.DisplayName, "selected source zero value must clear destination")
	require.NotNil(t, target.Address)
	assert.Equal(t, fieldMaskAddress{City: "Suzhou", Zip: "200000"}, *target.Address)
	assert.Equal(t, []fieldMaskItem{
		{Label: "source-first", Count: 1},
		{Label: "source-second", Count: 2},
	}, target.Items)
	assert.Equal(t, [2]fieldMaskItem{
		{Label: "fixed-first", Count: 30},
		{Label: "fixed-second", Count: 40},
	}, target.Fixed)
	assert.Equal(t, fieldMaskAddress{City: "Chengdu", Zip: "310000"}, target.Attributes["home"])
	assert.NotContains(t, target.Attributes, "gone")
	assert.Contains(t, target.Attributes, "work")
	assert.Equal(t, &fieldMaskAddress{City: "Guangzhou", Zip: "518000"}, target.PointerAttributes["work"])
	assert.Equal(t, []string{"source"}, target.Tags)

	source.Tags[0] = "aliased"
	assert.Equal(t, "aliased", target.Tags[0], "whole selected slices use Go assignment semantics")
}

func TestMizu_FieldMaskOverwritePointers(t *testing.T) {
	t.Run("allocates destination parent", func(t *testing.T) {
		source := fieldMaskProfile{Address: &fieldMaskAddress{City: "Wuhan", Zip: "ignored"}}
		target := fieldMaskProfile{}
		mask := mizu.Intersect[fieldMaskProfile](
			[]string{"address.city"},
			[]string{"address.city"},
		)
		require.NoError(t, mask.Overwrite(&source, &target))
		assert.Equal(t, &fieldMaskAddress{City: "Wuhan"}, target.Address)
	})

	t.Run("nil source clears selected child", func(t *testing.T) {
		source := fieldMaskProfile{}
		target := fieldMaskProfile{Address: &fieldMaskAddress{City: "old", Zip: "keep"}}
		mask := mizu.Intersect[fieldMaskProfile](
			[]string{"address.city"},
			[]string{"address.city"},
		)
		require.NoError(t, mask.Overwrite(&source, &target))
		assert.Equal(t, &fieldMaskAddress{Zip: "keep"}, target.Address)
	})
}

func TestMizu_FieldMaskOverwriteMissingMapKey(t *testing.T) {
	t.Run("nested selection clears only selected fields", func(t *testing.T) {
		source := fieldMaskProfile{Attributes: map[string]fieldMaskAddress{}}
		target := fieldMaskProfile{Attributes: map[string]fieldMaskAddress{
			"home": {City: "old", Zip: "keep"},
		}}
		mask := mizu.Intersect[fieldMaskProfile](
			[]string{"attributes.home.city"},
			[]string{"attributes.home.city"},
		)
		require.NoError(t, mask.Overwrite(&source, &target))
		assert.Equal(t, fieldMaskAddress{Zip: "keep"}, target.Attributes["home"])
	})

	t.Run("leaf selection deletes key", func(t *testing.T) {
		source := fieldMaskProfile{Attributes: map[string]fieldMaskAddress{}}
		target := fieldMaskProfile{Attributes: map[string]fieldMaskAddress{
			"home": {City: "old", Zip: "old"},
		}}
		mask := mizu.Intersect[fieldMaskProfile](
			[]string{"attributes.home"},
			[]string{"attributes.home"},
		)
		require.NoError(t, mask.Overwrite(&source, &target))
		assert.NotContains(t, target.Attributes, "home")
	})
}

func TestMizu_FieldMaskErrors(t *testing.T) {
	t.Run("unsupported type", func(t *testing.T) {
		value := 7
		mask := mizu.Intersect[int]([]string{"value"}, []string{"value"})
		assert.Empty(t, mask.Paths())
		require.ErrorContains(t, mask.Filter(&value), "must be a JSON struct")
		assert.Equal(t, 7, value)
	})

	t.Run("nil receiver", func(t *testing.T) {
		profile := newFieldMaskProfile()
		want := newFieldMaskProfile()
		var mask *mizu.FieldMask[fieldMaskProfile]
		require.ErrorContains(t, mask.Filter(&profile), "field mask is nil")
		assert.Equal(t, want, profile)
	})

	t.Run("nil overwrite source is atomic", func(t *testing.T) {
		target := newFieldMaskProfile()
		want := newFieldMaskProfile()
		mask := mizu.Intersect[fieldMaskProfile](
			[]string{"displayName"},
			[]string{"displayName"},
		)
		require.ErrorContains(t, mask.Overwrite(nil, &target), "source: value is nil")
		assert.Equal(t, want, target)
	})
}
