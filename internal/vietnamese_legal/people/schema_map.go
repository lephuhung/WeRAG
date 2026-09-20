// Package people ports AIRAG's MongoDB people-search service
// (app/services/people/) to Go. The searchable schema map is copied verbatim
// from mongo_searchable_map.py so behaviour matches the reference deployment
// against MongoDB 10.10.0.120.
package people

// SchemaConfig describes one MongoDB collection's searchable fields for a
// lookup type. Fields is used by exact/regex/phone lookups; CanonFields maps
// canonical attributes (name/dob/address/phone) onto collection fields for
// the advanced AND lookup; DisplayFields selects which fields are rendered.
type SchemaConfig struct {
	Fields        []string
	CanonFields   map[string]string // advanced lookup only
	DisplayFields []string
}

// QueryFields returns the field list for a non-advanced lookup.
func (c SchemaConfig) QueryFields() []string { return c.Fields }

// LookupConfig groups the collections searched for one lookup type.
type LookupConfig struct {
	Description string
	Collections map[string]SchemaConfig
}

// SearchableCollectionMap mirrors AIRAG SEARCHABLE_COLLECTION_MAP.
var SearchableCollectionMap = map[string]LookupConfig{
	"cccd": {
		Description: "Tìm theo số CCCD/CMND",
		Collections: map[string]SchemaConfig{
			"bhxh": {
				Fields:        []string{"soCmnd"},
				DisplayFields: []string{"hoTen", "maSoBhxh", "soTheBhyt", "ngaySinhHienThi", "trangThaiThe", "coSoKCB"},
			},
			"evn": {
				Fields:        []string{"cmnd"},
				DisplayFields: nil,
			},
			"lg": {
				Fields:        []string{"SoDinhDanh"},
				DisplayFields: []string{"TenHoiVien", "SoDienThoai", "NgaySinh", "DiaChi", "TenHangHoiVien", "SoTheHoiVien"},
			},
			"vacxin": {
				Fields:        []string{"MA_DOI_TUONG"},
				DisplayFields: []string{"HO_TEN", "NGAY_SINH", "TEN_ME", "DIEN_THOAI_ME", "GIOI_TINH", "PID"},
			},
		},
	},
	"bhxh": {
		Description: "Tìm theo số BHXH",
		Collections: map[string]SchemaConfig{
			"bhxh": {
				Fields:        []string{"maSoBhxh"},
				DisplayFields: []string{"hoTen", "soTheBhyt", "ngaySinhHienThi", "trangThaiThe", "tyLeBhyt", "tuNgay", "denNgay", "coSoKCB"},
			},
		},
	},
	"phone": {
		Description: "Tìm theo số điện thoại",
		Collections: map[string]SchemaConfig{
			"bhxh": {
				Fields:        []string{"soDienThoai"},
				DisplayFields: []string{"hoTen", "maSoBhxh", "soTheBhyt", "ngaySinhHienThi", "trangThaiThe", "coSoKCB"},
			},
			"evn": {
				Fields:        []string{"dienThoai"},
				DisplayFields: []string{"tenKhachHang", "cmnd", "diaChiCapDien", "ngayDangKy"},
			},
			"lg": {
				// The `lg` collection stores the phone on `phone` (indexed),
				// NOT `SoDienThoai` — querying that field collection-scans
				// ~13M docs and always times out.
				Fields:        []string{"phone"},
				DisplayFields: []string{"TenHoiVien", "SoDinhDanh"},
			},
			"vacxin": {
				Fields:        []string{"DIEN_THOAI_ME"},
				DisplayFields: []string{"HO_TEN", "NGAY_SINH", "TEN_ME", "DIEN_THOAI_ME", "GIOI_TINH", "PID"},
			},
			"cv19": {
				Fields:        []string{"so_dien_thoai"},
				DisplayFields: []string{"ho_ten", "so_dien_thoai", "namsinh", "gioi_tinh", "dia_chi"},
			},
			"uids": {
				Fields:        []string{"phone"},
				DisplayFields: []string{"uid", "phone"},
			},
			"vnvc": {
				Fields:        []string{"mobile"},
				DisplayFields: []string{"fullName", "fullNam", "mobile", "diaChi", "TEN_ME", "gioi_tinh"},
			},
		},
	},
	"name": {
		Description: "Tìm theo tên",
		Collections: map[string]SchemaConfig{
			"bhxh": {
				Fields:        []string{"hoTen"},
				DisplayFields: []string{"hoTen", "maSoBhxh", "soTheBhyt", "soCmnd", "soDienThoai", "ngaySinhHienThi"},
			},
			"lg": {
				Fields:        []string{"TenHoiVien"},
				DisplayFields: []string{"TenHoiVien", "SoDinhDanh", "SoDienThoai", "NgaySinh", "DiaChi"},
			},
			"vacxin": {
				Fields:        []string{"HO_TEN"},
				DisplayFields: []string{"HO_TEN", "MA_DOI_TUONG", "NGAY_SINH", "DIEN_THOAI_ME", "TEN_ME"},
			},
			"evn": {
				Fields:        []string{"tenKhachHang"},
				DisplayFields: []string{"tenKhachHang", "cmnd", "phone", "diaChiCapDien", "ngayDangKy"},
			},
			"cv19": {
				Fields:        []string{"ho_ten"},
				DisplayFields: []string{"ho_ten", "so_dien_thoai", "namsinh", "gioi_tinh", "dia_chi"},
			},
			"vnvc": {
				Fields:        []string{"fullName"},
				DisplayFields: []string{"fullName", "mobile", "fullNam", "diaChi", "TEN_ME"},
			},
		},
	},
	"advanced": {
		Description: "Tìm theo tổ hợp thông tin",
		Collections: map[string]SchemaConfig{
			"bhxh": {
				CanonFields:   map[string]string{"name": "hoTen", "dob": "ngaySinhHienThi", "address": "diaChi", "phone": "soDienThoai"},
				DisplayFields: []string{"hoTen", "maSoBhxh", "soTheBhyt", "soCmnd", "soDienThoai", "ngaySinhHienThi", "diaChi", "coSoKCB"},
			},
			"lg": {
				CanonFields:   map[string]string{"name": "TenHoiVien", "dob": "NgaySinh", "address": "DiaChi", "phone": "SoDienThoai"},
				DisplayFields: []string{"TenHoiVien", "SoDinhDanh", "SoDienThoai", "NgaySinh", "DiaChi"},
			},
			"vacxin": {
				CanonFields:   map[string]string{"name": "HO_TEN", "dob": "NGAY_SINH", "phone": "DIEN_THOAI_ME"},
				DisplayFields: []string{"HO_TEN", "MA_DOI_TUONG", "NGAY_SINH", "DIEN_THOAI_ME", "TEN_ME"},
			},
			"evn": {
				CanonFields:   map[string]string{"name": "tenKhachHang", "address": "diaChiCapDien", "phone": "phone"},
				DisplayFields: []string{"tenKhachHang", "cmnd", "phone", "diaChiCapDien", "ngayDangKy"},
			},
			"cv19": {
				CanonFields:   map[string]string{"name": "ho_ten", "dob": "namsinh", "address": "dia_chi", "phone": "so_dien_thoai"},
				DisplayFields: []string{"ho_ten", "so_dien_thoai", "namsinh", "gioi_tinh", "dia_chi"},
			},
			"vnvc": {
				CanonFields:   map[string]string{"name": "fullName", "dob": "fullNam", "address": "diaChi", "phone": "mobile"},
				DisplayFields: []string{"fullName", "mobile", "fullNam", "diaChi", "TEN_ME"},
			},
		},
	},
}

// schemaDescriptions mirrors AIRAG SCHEMA_DESCRIPTIONS.
var schemaDescriptions = map[string]string{
	"bhxh":   "Hồ sơ Bảo hiểm xã hội — thông tin BHXH, BHYT, thẻ y tế, cơ sở khám chữa bệnh",
	"evn":    "Hồ sơ điện lực — thông tin khách hàng điện lực",
	"lg":     "Thông tin thuê bao Vinaphone",
	"vacxin": "Hồ sơ tiêm chủng — thông tin tiêm chủng vaccine, đối tượng tiêm, thông tin phụ huynh",
	"cv19":   "Hồ sơ COVID-19 — thông tin xét nghiệm, tiêm vaccine COVID-19",
	"uids":   "Hồ sơ UID Facebook",
	"vnvc":   "Hồ sơ tiêm chủng VNVC — thông tin đăng ký tiêm vaccine tại VNVC",
}

// SchemaDisplayName returns the human description of a collection.
func SchemaDisplayName(schema string) string {
	if d, ok := schemaDescriptions[schema]; ok {
		return d
	}
	return schema
}
