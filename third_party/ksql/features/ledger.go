// Package features exposes the KQL feature inventory and SQL support status.
package features

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

// Support describes semantic support, not merely parser acceptance.
type Support string

const (
	Exact       Support = "exact"
	Equivalent  Support = "equivalent"
	Lossy       Support = "lossy"
	Unsupported Support = "unsupported"
)

// ParseSupport describes the parser's treatment of a feature.
type ParseSupport string

const (
	Validated  ParseSupport = "validated"
	Recognized ParseSupport = "recognized"
	Opaque     ParseSupport = "opaque"
)

// Entry describes one inventoried KQL construct.
type Entry struct {
	ID       string       `json:"id"`
	Category string       `json:"category"`
	Name     string       `json:"name"`
	Parse    ParseSupport `json:"parse"`
	Support  Support      `json:"support"`
	Notes    string       `json:"notes,omitempty"`
}

var (
	entriesOnce sync.Once
	entries     []Entry
	byID        map[string]Entry
)

// All returns an ID-sorted copy of the pinned feature inventory.
func All() []Entry {
	initEntries()
	return append([]Entry(nil), entries...)
}

// Lookup finds a feature by stable ID.
func Lookup(id string) (Entry, bool) {
	initEntries()
	entry, ok := byID[id]
	return entry, ok
}

// JSON returns an indented machine-readable copy of the ledger.
func JSON() []byte {
	data, _ := json.MarshalIndent(All(), "", "  ")
	return append(data, '\n')
}

func initEntries() {
	entriesOnce.Do(func() {
		add := func(category, prefix, names string, parse ParseSupport, support Support, notes string) {
			for _, name := range strings.Fields(names) {
				entry := Entry{ID: prefix + name, Category: category, Name: name, Parse: parse, Support: support, Notes: notes}
				entries = append(entries, entry)
			}
		}

		add("source", "source.", sourceNames, Recognized, Unsupported, "source availability and lowering are target-dependent")
		add("statement", "statement.", statementNames, Recognized, Unsupported, "session and control-plane statements are not SQL queries")
		add("operator", "operator.", operatorNames, Recognized, Unsupported, "recognized with a stable unsupported diagnostic")
		add("scalar_operator", "operator.scalar.", scalarOperatorNames, Validated, Unsupported, "support depends on SQL semantics and target capabilities")
		add("function", "function.", scalarFunctionNames, Validated, Unsupported, "requires an explicit semantic SQL mapping or UDF")
		add("conversion", "function.", conversionFunctionNames, Validated, Unsupported, "KQL conversions return null on failure")
		add("aggregate", "aggregate.", aggregateNames, Validated, Unsupported, "aggregate empty-set and dynamic-value semantics must be preserved")
		add("window", "function.", windowFunctionNames, Validated, Unsupported, "requires a serialized input order")
		add("plugin", "plugin.", pluginNames, Recognized, Unsupported, "evaluate plugins require an external runtime or relational adapter")

		setSupport := func(ids string, parse ParseSupport, support Support, notes string) {
			wanted := make(map[string]struct{})
			for _, id := range strings.Fields(ids) {
				wanted[id] = struct{}{}
			}
			for i := range entries {
				if _, ok := wanted[entries[i].ID]; ok {
					entries[i].Parse = parse
					entries[i].Support = support
					entries[i].Notes = notes
					delete(wanted, entries[i].ID)
				}
			}
		}

		setSupport(coreEquivalent, Validated, Equivalent, "lowered through the dialect-neutral SQL AST")
		setSupport(coreLossy, Validated, Lossy, "translation has a documented KQL/SQL semantic difference")

		sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
		byID = make(map[string]Entry, len(entries))
		for _, entry := range entries {
			// Scalar and aggregate registries intentionally overlap. Preserve the
			// first entry under an ID while retaining every registry row in All.
			if _, exists := byID[entry.ID]; !exists {
				byID[entry.ID] = entry
			}
		}
	})
}

const sourceNames = `
table expression print range datatable union externaldata inline_external_table
entity_group contextual_datatable materialized_view_combine
`

const statementNames = `
let expression alias declare restrict set query_parameters pattern
`

const operatorNames = `
where project project-away project-keep project-rename project-reorder
project-by-names extend summarize distinct count sort take top join lookup union
serialize as invoke find search parse parse-where parse-kv make-series mv-expand
mv-apply sample sample-distinct top-hitters top-nested reduce partition
__partitionby fork facet scan make-graph graph-match graph-shortest-paths
graph-mark-components graph-where-nodes graph-where-edges graph-to-table
macro-expand evaluate render getschema consume assert-schema __executeandcache
`

const scalarOperatorNames = `
unary_plus unary_minus and or add subtract multiply divide modulo less_than
less_than_or_equal greater_than greater_than_or_equal equal not_equal equal_tilde
bang_tilde has has_cs not_has not_has_cs hasprefix hasprefix_cs not_hasprefix
not_hasprefix_cs hassuffix hassuffix_cs not_hassuffix not_hassuffix_cs like
like_cs not_like not_like_cs contains contains_cs not_contains not_contains_cs
startswith startswith_cs not_startswith not_startswith_cs endswith endswith_cs
not_endswith not_endswith_cs matches_regex search in in_cs not_in not_in_cs
between not_between has_any has_all
`

// Names are transcribed from Functions.All at upstream commit c94c9e7.
const scalarFunctionNames = `
cluster database table external_table materialized_view entity_group
stored_query_result graph
strcat strcat_array array_strcat strcat_delim strcmp strrep strlen string_size
toupper tolower to_utf8 unicode_codepoints_from_string substring regex_quote
indexof indexof_regex has_any_index reverse split parse_command_line extract
extractall extract_all extractjson extract_json replace replace_regex
replace_string replace_strings trim_start trim_end trim countof translate
make_string unicode_codepoints_to_string datetime_local_to_utc
datetime_utc_to_local datetime_list_timezones
url_encode url_encode_component url_decode base64_encodestring
base64_encode_tostring base64_decode_toarray base64_encode_fromarray
base64_decodestring base64_decode_tostring base64_decode_toguid
base64_encode_fromguid zlib_decompress_from_base64_string
zlib_compress_to_base64_string gzip_decompress_from_base64_string
gzip_compress_to_base64_string __lz4_compress_dynamic_array_to_base64_string
parse_csv parsejson parse_json parse_xml parseurl parse_url parseurlquery
parse_urlquery parse_ipv4 parse_ipv4_mask parse_ipv6 parse_ipv6_mask parse_path
parse_user_agent parse_version
format_datetime format_timespan make_datetime make_timespan datetime_add
datetime_diff dayofweek dayofmonth dayofyear hourofday weekofyear week_of_year
monthofyear startofday startofweek startofmonth startofyear endofday endofweek
endofmonth endofyear getyear getmonth datepart datetime_part now ago
unixtime_seconds_todatetime unixtime_milliseconds_todatetime
unixtime_microseconds_todatetime unixtime_nanoseconds_todatetime
__hash_crc32 __hash_djb2 hash hash_sha256 hash_md5 hash_sha1 hash_xxhash64
__hash_xxh64 hash_combine hash_many __hash_many_crc32
iif iff case assert bin floor bin_at bin_auto not notnull isnotnull isnull
notempty iscolumnexists isascii isutf8 isnotempty isempty columnifexists
column_ifexists around binary_and binary_or binary_xor binary_not
binary_shift_right binary_shift_left bitset_count_ones
treepath repeat arraylength array_length range array_concat array_iif array_iff
array_index_of array_slice array_split array_shift_left array_shift_right
array_reverse array_rotate_left array_rotate_right array_sort_asc array_sort_desc
bag_keys zip pack pack_dictionary bag_pack bag_pack_columns pack_all pack_array
set_has_element set_union set_intersect set_difference set_equals bag_merge
dynamic_to_json bag_remove_keys bag_has_key jaccard_index bag_set_key bag_zip
punycode_to_string punycode_from_string punycode_domain_from_string
punycode_domain_to_string
percentile_tdigest percentiles_array_tdigest percentrank_tdigest rank_tdigest
tdigest_isvalid hll_isvalid tdigest_merge merge_tdigest hll_merge dcount_hll
__hll_normalize series_fir series_stats series_stats_dynamic series_fft
series_ifft series_fit_line series_fit_line_dynamic series_fit_2lines
series_fit_2lines_dynamic series_outliers series_iir series_periods_detect
series_periods_validate series_fill_backward series_fill_forward
series_fill_const series_fill_linear series_fit_poly series_add series_subtract
series_multiply series_divide series_pow series_greater series_greater_equals
series_less series_less_equals series_equals series_not_equals series_exp
series_sign series_abs series_sin series_asin series_cos series_acos series_tan
series_atan series_magnitude series_sum series_product series_log series_floor
series_ceiling array_sum series_seasonal series_decompose
series_decompose_forecast series_decompose_anomalies
series_pearson_correlation series_dot_product series_cosine_similarity
round ceiling pow sqrt log log2 log10 exp exp2 exp10 pi cos sin tan acos asin
atan atan2 abs cot degrees radians sign rand beta_cdf beta_inv beta_pdf gamma
loggamma erf erfc isnan isinf isfinite coalesce max_of min_of welch_test
geo_from_wkt geo_angle geo_azimuth geo_closest_point_on_line
geo_closest_point_on_polygon geo_distance_2points geo_distance_point_to_line
geo_distance_point_to_polygon geo_point_in_circle geo_point_in_polygon
geo_intersects_2lines geo_intersects_line_with_polygon geo_intersects_2polygons
geo_intersection_2lines geo_intersection_line_with_polygon
geo_intersection_2polygons geo_union_polygons_array geo_simplify_polygons_array
geo_union_lines_array geo_polygon_to_h3cells geo_polygon_to_s2cells
__geo_polygon_s2cell_covering_level geo_polygon_densify geo_polygon_area
geo_polygon_buffer geo_polygon_centroid __geo_polygon_validate
geo_polygon_perimeter geo_polygon_simplify __geo_line_s2cell_covering_level
geo_line_length geo_line_buffer geo_line_centroid geo_line_densify
geo_line_simplify geo_line_locate_point geo_line_interpolate_point
__geo_line_validate geo_line_to_s2cells __geo_length_to_s2cell_level
geo_point_to_geohash geo_geohash_to_central_point geo_geohash_to_polygon
geo_geohash_neighbors geo_point_buffer geo_point_to_s2cell
geo_s2cell_to_central_point geo_s2cell_to_polygon geo_s2cell_neighbors
geo_point_to_h3cell geo_h3cell_to_central_point geo_h3cell_to_polygon
geo_h3cell_neighbors geo_h3cell_children geo_h3cell_parent geo_h3cell_rings
geo_h3cell_level
all any map inner_nodes node_id node_degree_in node_degree_out labels
ipv4_compare ipv4_is_match ipv6_compare ipv4_is_private ipv6_is_match
ipv6_is_in_range ipv6_is_in_any_range ipv4_is_in_range ipv4_is_in_any_range
ipv4_netmask_suffix __ipv6_lookup_ranges format_ipv4 format_ipv4_mask format_bytes
current_cluster_endpoint current_database current_principal
current_principal_details current_principal_is_member_of extent_id extentid
extent_tags current_node_id ingestion_time row_id cursor_after cursor_before_or_at
cursor_current current_cursor has_ipv4 has_ipv4_prefix has_any_ipv4
has_any_ipv4_prefix ipv4_range_to_cidr_list rowstore_ordinal_range
estimate_data_size new_guid __invoke __cast geo_info_from_ip_address column_names_of
`

const conversionFunctionNames = `
tostring tohex todynamic toobject tolong toint toreal todouble todatetime
totimespan totime tobool toboolean todecimal toguid gettype
convert_angle convert_energy convert_force convert_length convert_mass
convert_speed convert_temperature convert_volume
`

const aggregateNames = `
sum sumif cnt count countif dcount dcountif tdigest tdigest_merge merge_tdigest
hll hll_if hll_merge min minif max maxif avg avgif makelist make_list
make_list_if make_list_with_nulls makeset make_set make_set_if make_dictionary
make_bag make_bag_if buildschema passthrough percentile percentiles
percentiles_array percentilew percentilesw percentilesw_array stdev stdevif
stdevp variance varianceif variancep variancepif covariance covarianceif
covariancep covariancepif any take_any anyif take_anyif arg_min arg_max argmin
argmax binary_all_or binary_all_and binary_all_xor count_distinct
count_distinctif
`

const windowFunctionNames = `
row_number row_cumsum row_rank row_rank_dense row_rank_min row_window_session
prev next
`

const pluginNames = `
active_users_count activity_counts_metrics activity_engagement activity_metrics
azure_digital_twins_query_request autocluster bag_unpack basket
cosmosdb_sql_request csharp dcount_intersect diffpatterns estimate_rows_count
execute_show_command execute_query external_datatable funnel_sequence
funnel_sequence_completion http_request http_request_post identity identity_v3
infer_storage_schema infer_storage_schema_with_suggestions geo_polygon_lookup
geo_line_lookup ipv4_lookup ipv6_lookup narrow new_activity_metrics pivot preview
python r rolling_percentile rows_near session_count sequence_detect
sliding_window_counts sql_request mysql_request postgresql_request dax_request
ai_embed_text ai_chat_completion ai_chat_completion_prompt ai_embeddings
`

const coreEquivalent = `
source.table source.print source.range source.datatable source.union statement.let statement.expression
operator.where operator.project operator.extend operator.summarize
operator.distinct operator.count operator.getschema operator.sort operator.take operator.top
operator.join operator.lookup operator.union operator.as operator.sample
operator.sample-distinct operator.project-away operator.project-keep
operator.project-rename operator.project-reorder operator.search
operator.scalar.and operator.scalar.or operator.scalar.add
operator.scalar.subtract operator.scalar.multiply operator.scalar.divide
operator.scalar.modulo operator.scalar.less_than operator.scalar.less_than_or_equal
operator.scalar.greater_than operator.scalar.greater_than_or_equal
operator.scalar.equal operator.scalar.not_equal operator.scalar.equal_tilde
operator.scalar.bang_tilde operator.scalar.contains operator.scalar.contains_cs
operator.scalar.not_contains operator.scalar.not_contains_cs
operator.scalar.startswith operator.scalar.startswith_cs
operator.scalar.not_startswith operator.scalar.not_startswith_cs
operator.scalar.endswith operator.scalar.endswith_cs operator.scalar.not_endswith
operator.scalar.not_endswith_cs operator.scalar.matches_regex operator.scalar.in
operator.scalar.not_in operator.scalar.in_cs operator.scalar.not_in_cs
operator.scalar.between operator.scalar.not_between
operator.scalar.has operator.scalar.has_cs operator.scalar.not_has
operator.scalar.not_has_cs operator.scalar.hasprefix operator.scalar.hasprefix_cs
operator.scalar.not_hasprefix operator.scalar.not_hasprefix_cs
operator.scalar.hassuffix operator.scalar.hassuffix_cs operator.scalar.not_hassuffix
operator.scalar.not_hassuffix_cs operator.scalar.has_any operator.scalar.has_all
function.not function.iif function.iff function.case function.strcat function.strlen
function.tolower function.toupper function.substring function.count function.sum
function.sumif function.countif function.avg function.min function.max
function.coalesce function.abs function.ceiling function.floor function.round
function.pow function.sqrt function.exp function.log function.log10 function.sin
function.cos function.tan function.asin function.acos function.atan
function.atan2 function.sign function.tostring
aggregate.count aggregate.sum aggregate.sumif aggregate.countif aggregate.avg
aggregate.min aggregate.max
`

const coreLossy = `
operator.serialize operator.mv-expand operator.mv-apply aggregate.make_list
function.toint function.tolong function.toreal
function.todouble function.todatetime function.todecimal function.tobool
function.toboolean function.toguid
`
