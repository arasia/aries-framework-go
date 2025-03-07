/*
Copyright SecureKey Technologies Inc. All Rights Reserved.

SPDX-License-Identifier: Apache-2.0
*/

package presexch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/PaesslerAG/jsonpath"
	"github.com/google/uuid"
	jsonpathkeys "github.com/kawamuray/jsonpath"
	"github.com/piprate/json-gold/ld"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
	"github.com/xeipuuv/gojsonschema"

	"github.com/hyperledger/aries-framework-go/pkg/common/log"
	"github.com/hyperledger/aries-framework-go/pkg/doc/jose"
	"github.com/hyperledger/aries-framework-go/pkg/doc/jwt"
	"github.com/hyperledger/aries-framework-go/pkg/doc/sdjwt/common"
	"github.com/hyperledger/aries-framework-go/pkg/doc/verifiable"
)

const (

	// All rule`s value.
	All Selection = "all"
	// Pick rule`s value.
	Pick Selection = "pick"

	// Required predicate`s value.
	Required Preference = "required"
	// Preferred predicate`s value.
	Preferred Preference = "preferred"

	tmpEnding = "tmp_unique_id_"

	credentialSchema = "credentialSchema"

	// FormatJWT presentation exchange format.
	FormatJWT = "jwt"
	// FormatJWTVC presentation exchange format.
	FormatJWTVC = "jwt_vc"
	// FormatJWTVP presentation exchange format.
	FormatJWTVP = "jwt_vp"
	// FormatLDP presentation exchange format.
	FormatLDP = "ldp"
	// FormatLDPVC presentation exchange format.
	FormatLDPVC = "ldp_vc"
	// FormatLDPVP presentation exchange format.
	FormatLDPVP = "ldp_vp"
)

var errPathNotApplicable = errors.New("path not applicable")

var logger = log.New("doc/presexch")

type (
	// Selection can be "all" or "pick".
	Selection string
	// Preference can be "required" or "preferred".
	Preference string
	// StrOrInt type that defines string or integer.
	StrOrInt interface{}
)

func (v *Preference) isRequired() bool {
	if v == nil {
		return false
	}

	return *v == Required
}

// Format describes PresentationDefinition`s Format field.
type Format struct {
	Jwt   *JwtType `json:"jwt,omitempty"`
	JwtVC *JwtType `json:"jwt_vc,omitempty"`
	JwtVP *JwtType `json:"jwt_vp,omitempty"`
	Ldp   *LdpType `json:"ldp,omitempty"`
	LdpVC *LdpType `json:"ldp_vc,omitempty"`
	LdpVP *LdpType `json:"ldp_vp,omitempty"`
}

func (f *Format) notNil() bool {
	return f != nil &&
		(f.Jwt != nil || f.JwtVC != nil || f.JwtVP != nil || f.Ldp != nil || f.LdpVC != nil || f.LdpVP != nil)
}

// JwtType contains alg.
type JwtType struct {
	Alg []string `json:"alg,omitempty"`
}

// LdpType contains proof_type.
type LdpType struct {
	ProofType []string `json:"proof_type,omitempty"`
}

// PresentationDefinition presentation definitions (https://identity.foundation/presentation-exchange/).
type PresentationDefinition struct {
	// ID unique resource identifier.
	ID string `json:"id,omitempty"`
	// Name human-friendly name that describes what the Presentation Definition pertains to.
	Name string `json:"name,omitempty"`
	// Purpose describes the purpose for which the Presentation Definition’s inputs are being requested.
	Purpose string `json:"purpose,omitempty"`
	Locale  string `json:"locale,omitempty"`
	// Format is an object with one or more properties matching the registered Claim Format Designations
	// (jwt, jwt_vc, jwt_vp, etc.) to inform the Holder of the claim format configurations the Verifier can process.
	Format *Format `json:"format,omitempty"`
	// Frame is used for JSON-LD document framing.
	Frame map[string]interface{} `json:"frame,omitempty"`
	// SubmissionRequirements must conform to the Submission Requirement Format.
	// If not present, all inputs listed in the InputDescriptors array are required for submission.
	SubmissionRequirements []*SubmissionRequirement `json:"submission_requirements,omitempty"`
	InputDescriptors       []*InputDescriptor       `json:"input_descriptors,omitempty"`
}

// SubmissionRequirement describes input that must be submitted via a Presentation Submission
// to satisfy Verifier demands.
type SubmissionRequirement struct {
	Name       string                   `json:"name,omitempty"`
	Purpose    string                   `json:"purpose,omitempty"`
	Rule       Selection                `json:"rule,omitempty"`
	Count      int                      `json:"count,omitempty"`
	Min        int                      `json:"min,omitempty"`
	Max        int                      `json:"max,omitempty"`
	From       string                   `json:"from,omitempty"`
	FromNested []*SubmissionRequirement `json:"from_nested,omitempty"`
}

// InputDescriptor input descriptors.
type InputDescriptor struct {
	ID          string                 `json:"id,omitempty"`
	Group       []string               `json:"group,omitempty"`
	Name        string                 `json:"name,omitempty"`
	Purpose     string                 `json:"purpose,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
	Schema      []*Schema              `json:"schema,omitempty"`
	Constraints *Constraints           `json:"constraints,omitempty"`
	Format      *Format                `json:"format,omitempty"`
}

// Schema input descriptor schema.
type Schema struct {
	URI      string `json:"uri,omitempty"`
	Required bool   `json:"required,omitempty"`
}

// Holder describes Constraints`s  holder object.
type Holder struct {
	FieldID   []string    `json:"field_id,omitempty"`
	Directive *Preference `json:"directive,omitempty"`
}

// Constraints describes InputDescriptor`s Constraints field.
type Constraints struct {
	LimitDisclosure *Preference `json:"limit_disclosure,omitempty"`
	SubjectIsIssuer *Preference `json:"subject_is_issuer,omitempty"`
	IsHolder        []*Holder   `json:"is_holder,omitempty"`
	Fields          []*Field    `json:"fields,omitempty"`
}

// Field describes Constraints`s Fields field.
type Field struct {
	Path           []string    `json:"path,omitempty"`
	ID             string      `json:"id,omitempty"`
	Purpose        string      `json:"purpose,omitempty"`
	Filter         *Filter     `json:"filter,omitempty"`
	Predicate      *Preference `json:"predicate,omitempty"`
	IntentToRetain bool        `json:"intent_to_retain,omitempty"`
}

// Filter describes filter.
type Filter struct {
	Type             *string                `json:"type,omitempty"`
	Format           string                 `json:"format,omitempty"`
	Pattern          string                 `json:"pattern,omitempty"`
	Minimum          StrOrInt               `json:"minimum,omitempty"`
	Maximum          StrOrInt               `json:"maximum,omitempty"`
	MinLength        int                    `json:"minLength,omitempty"`
	MaxLength        int                    `json:"maxLength,omitempty"`
	ExclusiveMinimum StrOrInt               `json:"exclusiveMinimum,omitempty"`
	ExclusiveMaximum StrOrInt               `json:"exclusiveMaximum,omitempty"`
	Const            StrOrInt               `json:"const,omitempty"`
	Enum             []StrOrInt             `json:"enum,omitempty"`
	Not              map[string]interface{} `json:"not,omitempty"`
	Contains         map[string]interface{} `json:"contains,omitempty"`
}

// MatchedSubmissionRequirement contains information about VCs that matched a presentation definition.
type MatchedSubmissionRequirement struct {
	Name        string
	Purpose     string
	Rule        Selection
	Count       int
	Min         int
	Max         int
	Descriptors []*MatchedInputDescriptor
	Nested      []*MatchedSubmissionRequirement
}

// MatchedInputDescriptor contains information about VCs that matched an input descriptor of presentation definition.
type MatchedInputDescriptor struct {
	ID         string
	Name       string
	Purpose    string
	MatchedVCs []*verifiable.Credential
}

// ValidateSchema validates presentation definition.
func (pd *PresentationDefinition) ValidateSchema() error {
	result, err := gojsonschema.Validate(
		gojsonschema.NewStringLoader(DefinitionJSONSchemaV1),
		gojsonschema.NewGoLoader(struct {
			PD *PresentationDefinition `json:"presentation_definition"`
		}{PD: pd}),
	)

	if err != nil || !result.Valid() {
		result, err = gojsonschema.Validate(
			gojsonschema.NewStringLoader(DefinitionJSONSchemaV2),
			gojsonschema.NewGoLoader(struct {
				PD *PresentationDefinition `json:"presentation_definition"`
			}{PD: pd}),
		)
	}

	if err != nil {
		return err
	}

	if result.Valid() {
		return nil
	}

	resultErrors := result.Errors()

	errs := make([]string, len(resultErrors))
	for i := range resultErrors {
		errs[i] = resultErrors[i].String()
	}

	return errors.New(strings.Join(errs, ","))
}

type requirement struct {
	Name             string
	Purpose          string
	Rule             Selection
	Count            int
	Min              int
	Max              int
	InputDescriptors []*InputDescriptor
	Nested           []*requirement
}

func (r *requirement) isLenApplicable(val int) bool {
	if r.Count > 0 && val != r.Count {
		return false
	}

	if r.Min > 0 && r.Min > val {
		return false
	}

	if r.Max > 0 && r.Max < val {
		return false
	}

	return true
}

func contains(data []string, e string) bool {
	for _, el := range data {
		if el == e {
			return true
		}
	}

	return false
}

func toRequirement(sr *SubmissionRequirement, descriptors []*InputDescriptor) (*requirement, error) {
	var (
		inputDescriptors []*InputDescriptor
		nested           []*requirement
	)

	var totalCount int

	if sr.From != "" {
		for _, descriptor := range descriptors {
			if contains(descriptor.Group, sr.From) {
				inputDescriptors = append(inputDescriptors, descriptor)
			}
		}

		totalCount = len(inputDescriptors)
		if totalCount == 0 {
			return nil, fmt.Errorf("no descriptors for from: %s", sr.From)
		}
	} else {
		for _, sReq := range sr.FromNested {
			req, err := toRequirement(sReq, descriptors)
			if err != nil {
				return nil, err
			}
			nested = append(nested, req)
		}

		totalCount = len(nested)
	}

	count := sr.Count

	if sr.Rule == All {
		count = totalCount
	}

	return &requirement{
		Name:             sr.Name,
		Purpose:          sr.Purpose,
		Rule:             sr.Rule,
		Count:            count,
		Min:              sr.Min,
		Max:              sr.Max,
		InputDescriptors: inputDescriptors,
		Nested:           nested,
	}, nil
}

func makeRequirement(requirements []*SubmissionRequirement, descriptors []*InputDescriptor) (*requirement, error) {
	if len(requirements) == 0 {
		return &requirement{
			Count:            len(descriptors),
			InputDescriptors: descriptors,
		}, nil
	}

	req := &requirement{
		Count: len(requirements),
	}

	for _, submissionRequirement := range requirements {
		r, err := toRequirement(submissionRequirement, descriptors)
		if err != nil {
			return nil, err
		}

		req.Nested = append(req.Nested, r)
	}

	return req, nil
}

// CreateVP creates verifiable presentation.
func (pd *PresentationDefinition) CreateVP(credentials []*verifiable.Credential,
	documentLoader ld.DocumentLoader, opts ...verifiable.CredentialOpt) (*verifiable.Presentation, error) {
	if err := pd.ValidateSchema(); err != nil {
		return nil, err
	}

	req, err := makeRequirement(pd.SubmissionRequirements, pd.InputDescriptors)
	if err != nil {
		return nil, err
	}

	format, result, err := pd.applyRequirement(req, credentials, documentLoader, opts...)
	if err != nil {
		return nil, err
	}

	applicableCredentials, descriptors := merge(format, result)

	vp, err := verifiable.NewPresentation(verifiable.WithCredentials(applicableCredentials...))
	if err != nil {
		return nil, err
	}

	vp.Context = append(vp.Context, PresentationSubmissionJSONLDContextIRI)
	vp.Type = append(vp.Type, PresentationSubmissionJSONLDType)

	vp.CustomFields = verifiable.CustomFields{
		submissionProperty: &PresentationSubmission{
			ID:            uuid.New().String(),
			DefinitionID:  pd.ID,
			DescriptorMap: descriptors,
		},
	}

	return vp, nil
}

func makeRequirementsForMatch(requirements []*SubmissionRequirement,
	descriptors []*InputDescriptor) ([]*requirement, error) {
	if len(requirements) == 0 {
		return []*requirement{{
			Name:             "",
			Purpose:          "",
			Rule:             All,
			Count:            len(descriptors),
			InputDescriptors: descriptors,
			Nested:           nil,
		}}, nil
	}

	var reqs []*requirement

	for _, submissionRequirement := range requirements {
		r, err := toRequirement(submissionRequirement, descriptors)
		if err != nil {
			return nil, err
		}

		reqs = append(reqs, r)
	}

	return reqs, nil
}

// MatchSubmissionRequirement return information about matching VCs.
func (pd *PresentationDefinition) MatchSubmissionRequirement(credentials []*verifiable.Credential,
	documentLoader ld.DocumentLoader, opts ...verifiable.CredentialOpt) ([]*MatchedSubmissionRequirement, error) {
	if err := pd.ValidateSchema(); err != nil {
		return nil, err
	}

	requirements, err := makeRequirementsForMatch(pd.SubmissionRequirements, pd.InputDescriptors)
	if err != nil {
		return nil, err
	}

	var matchedReqs []*MatchedSubmissionRequirement

	for _, req := range requirements {
		matched, err := pd.matchRequirement(req, credentials, documentLoader, opts...)
		if err != nil {
			return nil, err
		}

		matchedReqs = append(matchedReqs, matched)
	}

	return matchedReqs, nil
}

// ErrNoCredentials when any credentials do not satisfy requirements.
var ErrNoCredentials = errors.New("credentials do not satisfy requirements")

func (pd *PresentationDefinition) matchRequirement(req *requirement, creds []*verifiable.Credential,
	documentLoader ld.DocumentLoader,
	opts ...verifiable.CredentialOpt) (*MatchedSubmissionRequirement, error) {
	matchedReq := &MatchedSubmissionRequirement{
		Name:        req.Name,
		Purpose:     req.Purpose,
		Rule:        req.Rule,
		Count:       req.Count,
		Min:         req.Min,
		Max:         req.Max,
		Descriptors: nil,
		Nested:      nil,
	}

	if len(req.InputDescriptors) != 0 {
		for _, descriptor := range req.InputDescriptors {
			_, filtered, err := pd.filterCredentialsThatMatchDescriptor(
				creds, descriptor, documentLoader, opts...)

			if err != nil {
				return nil, err
			}

			matchedReq.Descriptors = append(matchedReq.Descriptors, &MatchedInputDescriptor{
				ID:         descriptor.ID,
				Name:       descriptor.Name,
				Purpose:    descriptor.Purpose,
				MatchedVCs: filtered,
			})
		}
	}

	for _, nestedReq := range req.Nested {
		nestedMatch, err := pd.matchRequirement(nestedReq, creds, documentLoader, opts...)
		if err != nil {
			return nil, err
		}

		matchedReq.Nested = append(matchedReq.Nested, nestedMatch)
	}

	return matchedReq, nil
}

// nolint: gocyclo,funlen,gocognit
func (pd *PresentationDefinition) applyRequirement(req *requirement, creds []*verifiable.Credential,
	documentLoader ld.DocumentLoader,
	opts ...verifiable.CredentialOpt) (string, map[string][]*verifiable.Credential, error) {
	result := make(map[string][]*verifiable.Credential)
	// assume LDPVP format if pd.Format is not set.
	// Usually pd.Format will be set when creds include a non-empty Proofs field since they represent the designated
	// format.
	vpFormat := FormatLDPVP

	for _, descriptor := range req.InputDescriptors {
		descFormat, filtered, err := pd.filterCredentialsThatMatchDescriptor(
			creds, descriptor, documentLoader, opts...)

		if err != nil {
			return "", nil, err
		}

		if descFormat != "" {
			vpFormat = descFormat
		}

		if len(filtered) != 0 {
			result[descriptor.ID] = filtered
		}
	}

	if len(req.InputDescriptors) != 0 {
		if req.isLenApplicable(len(result)) {
			return vpFormat, result, nil
		}

		return "", nil, ErrNoCredentials
	}

	var nestedResult []map[string][]*verifiable.Credential

	// maps credential to descriptors that satisfy requirements
	set := map[string]map[string]string{}

	for _, r := range req.Nested {
		vpFmt, res, err := pd.applyRequirement(r, creds, documentLoader, opts...)
		if errors.Is(err, ErrNoCredentials) {
			continue
		}

		if err != nil {
			return "", nil, err
		}

		for desc, credentials := range res {
			for _, cred := range credentials {
				if _, ok := set[trimTmpID(cred.ID)]; !ok {
					set[trimTmpID(cred.ID)] = map[string]string{}
				}

				set[trimTmpID(cred.ID)][desc] = cred.ID
			}
		}

		if len(res) != 0 {
			nestedResult = append(nestedResult, res)
			vpFormat = vpFmt
		}
	}

	exclude := map[string]struct{}{}

	for k := range set {
		if !req.isLenApplicable(len(set[k])) {
			for desc, cID := range set[k] {
				exclude[desc+cID] = struct{}{}
			}
		}
	}

	return vpFormat, mergeNestedResult(nestedResult, exclude), nil
}

func (pd *PresentationDefinition) filterCredentialsThatMatchDescriptor(creds []*verifiable.Credential,
	descriptor *InputDescriptor,
	documentLoader ld.DocumentLoader,
	opts ...verifiable.CredentialOpt) (string, []*verifiable.Credential, error) {
	format := pd.Format
	if descriptor.Format.notNil() {
		format = descriptor.Format
	}

	vpFormat := ""

	filtered, err := frameCreds(pd.Frame, creds, opts...)
	if err != nil {
		return "", nil, err
	}

	if format.notNil() {
		vpFormat, filtered = filterFormat(format, filtered)
	}

	// Validate schema only for v1
	if descriptor.Schema != nil {
		filtered = filterSchema(descriptor.Schema, filtered, documentLoader)
	}

	filtered, err = filterConstraints(descriptor.Constraints, filtered, opts...)
	if err != nil {
		return "", nil, err
	}

	return vpFormat, filtered, nil
}

func mergeNestedResult(nr []map[string][]*verifiable.Credential,
	exclude map[string]struct{}) map[string][]*verifiable.Credential {
	result := make(map[string][]*verifiable.Credential)

	for _, res := range nr {
		for key, credentials := range res {
			set := map[string]struct{}{}

			var mergedCredentials []*verifiable.Credential

			for _, credential := range result[key] {
				if _, ok := set[credential.ID]; !ok {
					mergedCredentials = append(mergedCredentials, credential)
					set[credential.ID] = struct{}{}
				}
			}

			for _, credential := range credentials {
				if _, ok := set[credential.ID]; !ok {
					if _, exist := exclude[key+credential.ID]; !exist {
						mergedCredentials = append(mergedCredentials, credential)
						set[credential.ID] = struct{}{}
					}
				}
			}

			result[key] = mergedCredentials
		}
	}

	return result
}

func getSubjectIDs(subject interface{}) []string { // nolint: gocyclo
	switch s := subject.(type) {
	case string:
		return []string{s}
	case []map[string]interface{}:
		var res []string

		for i := range s {
			v, ok := s[i]["id"]
			if !ok {
				continue
			}

			sID, ok := v.(string)
			if !ok {
				continue
			}

			res = append(res, sID)
		}

		return res
	case map[string]interface{}:
		v, ok := s["id"]
		if !ok {
			return nil
		}

		sID, ok := v.(string)
		if !ok {
			return nil
		}

		return []string{sID}
	case verifiable.Subject:
		return []string{s.ID}

	case []verifiable.Subject:
		var res []string
		for i := range s {
			res = append(res, s[i].ID)
		}

		return res
	}

	return nil
}

func subjectIsIssuer(credential *verifiable.Credential) bool {
	for _, ID := range getSubjectIDs(credential.Subject) {
		if ID != "" && ID == credential.Issuer.ID {
			return true
		}
	}

	return false
}

// nolint: gocyclo,funlen,gocognit
func filterConstraints(constraints *Constraints, creds []*verifiable.Credential,
	opts ...verifiable.CredentialOpt) ([]*verifiable.Credential, error) {
	if constraints == nil {
		return creds, nil
	}

	var result []*verifiable.Credential

	for _, credential := range creds {
		if constraints.SubjectIsIssuer.isRequired() && !subjectIsIssuer(credential) {
			continue
		}

		var applicable bool

		// if credential.JWT is set, credential will marshal to a JSON string.
		// temporarily clear credential.JWT to avoid this.

		var err error

		credJWT := credential.JWT

		credentialWithFieldValues := credential

		if credential.SDJWTHashAlg != "" {
			credentialWithFieldValues, err = credential.CreateDisplayCredential(verifiable.DisplayAllDisclosures())
			if err != nil {
				continue
			}
		}

		// if credential.JWT is set, credential will marshal to a JSON string.
		// temporarily clear credential.JWT to avoid this.
		credentialWithFieldValues.JWT = ""

		credentialSrc, err := json.Marshal(credentialWithFieldValues)
		if err != nil {
			continue
		}

		credentialWithFieldValues.JWT = credJWT

		var credentialMap map[string]interface{}

		err = json.Unmarshal(credentialSrc, &credentialMap)
		if err != nil {
			return nil, err
		}

		var predicate bool

		for i, field := range constraints.Fields {
			err = filterField(field, credentialMap)
			if errors.Is(err, errPathNotApplicable) {
				applicable = false

				break
			}

			if err != nil {
				return nil, fmt.Errorf("filter field.%d: %w", i, err)
			}

			if field.Predicate.isRequired() {
				predicate = true
			}

			applicable = true
		}

		if !applicable {
			continue
		}

		if (constraints.LimitDisclosure.isRequired() || predicate) && credential.SDJWTHashAlg == "" {
			template := credentialSrc

			var contexts []interface{}

			for _, ctx := range credential.Context {
				contexts = append(contexts, ctx)
			}

			contexts = append(contexts, credential.CustomContext...)

			if constraints.LimitDisclosure.isRequired() {
				template, err = json.Marshal(map[string]interface{}{
					"id":                credential.ID,
					"type":              credential.Types,
					"@context":          contexts,
					"issuer":            credential.Issuer,
					"credentialSubject": toSubject(credential.Subject),
					"issuanceDate":      credential.Issued,
				})
				if err != nil {
					return nil, err
				}
			}

			var err error

			credential, err = createNewCredential(constraints, credentialSrc, template, credential, opts...)
			if err != nil {
				return nil, fmt.Errorf("create new credential: %w", err)
			}

			credential.ID = tmpID(credential.ID)
		}

		if constraints.LimitDisclosure.isRequired() && credential.SDJWTHashAlg != "" {
			limitedDisclosures, err := getLimitedDisclosures(constraints, credentialSrc, credential)
			if err != nil {
				return nil, err
			}

			credential.SDJWTDisclosures = limitedDisclosures
		}

		result = append(result, credential)
	}

	return result, nil
}

// nolint: gocyclo,funlen,gocognit
func getLimitedDisclosures(constraints *Constraints, displaySrc []byte, credential *verifiable.Credential) ([]*common.DisclosureClaim, error) { // nolint:lll
	hash, err := common.GetCryptoHash(credential.SDJWTHashAlg)
	if err != nil {
		return nil, err
	}

	vcJWT := credential.JWT
	credential.JWT = ""

	credentialSrc, err := json.Marshal(credential)
	if err != nil {
		return nil, err
	}

	// revert JWT to original value
	credential.JWT = vcJWT

	var limitedDisclosures []*common.DisclosureClaim

	for _, f := range constraints.Fields {
		jPaths, err := getJSONPaths(f.Path, displaySrc)
		if err != nil {
			return nil, err
		}

		for _, path := range jPaths {
			if strings.Contains(path[0], credentialSchema) {
				continue
			}

			parentPath := ""

			key := path[1]

			pathParts := strings.Split(path[1], ".")
			if len(pathParts) > 1 {
				parentPath = strings.Join(pathParts[:len(pathParts)-1], ".")
				key = pathParts[len(pathParts)-1]
			}

			parentObj, ok := gjson.GetBytes(credentialSrc, parentPath).Value().(map[string]interface{})
			if !ok {
				// no selective disclosures at this level, so nothing to add to limited disclosures
				continue
			}

			digests, err := common.GetDisclosureDigests(parentObj)
			if err != nil {
				return nil, err
			}

			for _, dc := range credential.SDJWTDisclosures {
				if dc.Name == key {
					digest, err := common.GetHash(hash, dc.Disclosure)
					if err != nil {
						return nil, err
					}

					if _, ok := digests[digest]; ok {
						limitedDisclosures = append(limitedDisclosures, dc)
					}
				}
			}
		}
	}

	return limitedDisclosures, nil
}

func frameCreds(frame map[string]interface{}, creds []*verifiable.Credential,
	opts ...verifiable.CredentialOpt) ([]*verifiable.Credential, error) {
	if frame == nil {
		return creds, nil
	}

	var result []*verifiable.Credential

	for _, credential := range creds {
		bbsVC, err := credential.GenerateBBSSelectiveDisclosure(frame, nil, opts...)
		if err != nil {
			return nil, err
		}

		result = append(result, bbsVC)
	}

	return result, nil
}

func toSubject(subject interface{}) interface{} {
	sub, ok := subject.([]verifiable.Subject)
	if ok && len(sub) == 1 {
		return verifiable.Subject{ID: sub[0].ID}
	}

	return subject
}

func tmpID(id string) string {
	return id + tmpEnding + uuid.New().String()
}

func trimTmpID(id string) string {
	idx := strings.Index(id, tmpEnding)
	if idx == -1 {
		return id
	}

	return id[:idx]
}

// nolint: funlen,gocognit,gocyclo
func createNewCredential(constraints *Constraints, src, limitedCred []byte,
	credential *verifiable.Credential, opts ...verifiable.CredentialOpt) (*verifiable.Credential, error) {
	var (
		BBSSupport          = hasBBS(credential)
		modifiedByPredicate bool
		explicitPaths       = make(map[string]bool)
	)

	for _, f := range constraints.Fields {
		jPaths, err := getJSONPaths(f.Path, src)
		if err != nil {
			return nil, err
		}

		for _, path := range jPaths {
			if strings.Contains(path[0], credentialSchema) {
				continue
			}

			var val interface{} = true

			if !modifiedByPredicate {
				modifiedByPredicate = f.Predicate.isRequired()
			}

			if f.Predicate == nil || *f.Predicate != Required {
				val = gjson.GetBytes(src, path[1]).Value()
			}

			if constraints.LimitDisclosure.isRequired() && BBSSupport {
				chunks := strings.Split(path[0], ".")
				explicitPath := strings.Join(chunks[:len(chunks)-1], ".")
				explicitPaths[explicitPath] = true
			}

			limitedCred, err = sjson.SetBytes(limitedCred, path[0], val)
			if err != nil {
				return nil, err
			}
		}
	}

	if !constraints.LimitDisclosure.isRequired() || !BBSSupport || modifiedByPredicate {
		opts = append(opts, verifiable.WithDisabledProofCheck())
		return verifiable.ParseCredential(limitedCred, opts...)
	}

	limitedCred, err := enhanceRevealDoc(explicitPaths, limitedCred, src)
	if err != nil {
		return nil, err
	}

	var doc map[string]interface{}
	if err := json.Unmarshal(limitedCred, &doc); err != nil {
		return nil, err
	}

	return credential.GenerateBBSSelectiveDisclosure(doc, []byte(uuid.New().String()), opts...)
}

func getJSONPaths(keys []string, src []byte) ([][2]string, error) {
	paths, err := jsonpathkeys.ParsePaths(keys...)
	if err != nil {
		return nil, err
	}

	eval, err := jsonpathkeys.EvalPathsInReader(bytes.NewReader(src), paths)
	if err != nil {
		return nil, err
	}

	var jPaths [][2]string

	set := map[string]int{}

	for {
		result, ok := eval.Next()
		if !ok {
			break
		}

		jPaths = append(jPaths, getPath(result.Keys, set))
	}

	return jPaths, nil
}

func enhanceRevealDoc(explicitPaths map[string]bool, limitedCred, vcBytes []byte) ([]byte, error) {
	var err error

	limitedCred, err = sjson.SetBytes(limitedCred, "@explicit", true)
	if err != nil {
		return nil, err
	}

	intermPaths := make(map[string]bool)

	for path, fromConstraint := range explicitPaths {
		limitedCred, err = enhanceRevealField(path, limitedCred, vcBytes)
		if err != nil {
			return nil, err
		}

		if !fromConstraint {
			continue
		}

		pathParts := strings.Split(path, ".")
		combinedPath := ""

		for i := 0; i < len(pathParts)-1; i++ {
			if i == 0 {
				combinedPath = pathParts[0]
			} else {
				combinedPath += "." + pathParts[i]
			}

			if _, ok := explicitPaths[combinedPath]; !ok {
				intermPaths[combinedPath] = false
			}
		}
	}

	for path := range intermPaths {
		limitedCred, err = enhanceRevealField(path, limitedCred, vcBytes)
		if err != nil {
			return nil, err
		}
	}

	return limitedCred, nil
}

func enhanceRevealField(path string, limitedCred, vcBytes []byte) ([]byte, error) {
	var err error

	limitedCred, err = sjson.SetBytes(limitedCred, path+".@explicit", true)
	if err != nil {
		return nil, err
	}

	for _, cf := range [...]string{"type", "@context"} {
		specialFieldPath := path + "." + cf

		specialFieldValue := gjson.GetBytes(vcBytes, specialFieldPath)
		if specialFieldValue.Type == gjson.Null {
			continue
		}

		limitedCred, err = sjson.SetBytes(limitedCred, specialFieldPath, specialFieldValue.Value())
		if err != nil {
			return nil, err
		}
	}

	return limitedCred, nil
}

func hasBBS(vc *verifiable.Credential) bool {
	for _, proof := range vc.Proofs {
		if proof["type"] == "BbsBlsSignature2020" {
			return true
		}
	}

	return false
}

func hasProofWithType(vc *verifiable.Credential, proofType string) bool {
	for _, proof := range vc.Proofs {
		if proof["type"] == proofType {
			return true
		}
	}

	return false
}

func filterField(f *Field, credential map[string]interface{}) error {
	var schema gojsonschema.JSONLoader

	if f.Filter != nil {
		schema = gojsonschema.NewGoLoader(*f.Filter)
	}

	var lastErr error

	for _, path := range f.Path {
		patch, err := jsonpath.Get(path, credential)
		if err == nil {
			err = validatePatch(schema, patch)
			if err == nil {
				return nil
			}

			lastErr = err
		} else {
			lastErr = errPathNotApplicable
		}
	}

	return lastErr
}

func validatePatch(schema gojsonschema.JSONLoader, patch interface{}) error {
	if schema == nil {
		return nil
	}

	raw, err := json.Marshal(patch)
	if err != nil {
		return err
	}

	result, err := gojsonschema.Validate(schema, gojsonschema.NewBytesLoader(raw))
	if err != nil || !result.Valid() {
		return errPathNotApplicable
	}

	return nil
}

func getPath(keys []interface{}, set map[string]int) [2]string {
	var (
		newPath      []string
		originalPath []string
	)

	for _, k := range keys {
		switch v := k.(type) {
		case int:
			counterKey := strings.Join(originalPath, ".")
			originalPath = append(originalPath, fmt.Sprintf("%d", v))
			mapperKey := strings.Join(originalPath, ".")

			if _, ok := set[mapperKey]; !ok {
				set[mapperKey] = set[counterKey]
				set[counterKey]++
			}

			newPath = append(newPath, fmt.Sprintf("%d", set[mapperKey]))
		default:
			originalPath = append(originalPath, fmt.Sprintf("%s", v))
			newPath = append(newPath, fmt.Sprintf("%s", v))
		}
	}

	return [...]string{strings.Join(newPath, "."), strings.Join(originalPath, ".")}
}

func merge(presentationFormat string, setOfCredentials map[string][]*verifiable.Credential) ([]*verifiable.Credential, []*InputDescriptorMapping) { //nolint:lll
	setOfCreds := make(map[string]int)
	setOfDescriptors := make(map[string]struct{})

	var (
		result      []*verifiable.Credential
		descriptors []*InputDescriptorMapping
	)

	keys := make([]string, 0, len(setOfCredentials))
	for k := range setOfCredentials {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	for _, descriptorID := range keys {
		credentials := setOfCredentials[descriptorID]

		for _, credential := range credentials {
			if _, ok := setOfCreds[credential.ID]; !ok {
				credential.ID = trimTmpID(credential.ID)
				result = append(result, credential)
				setOfCreds[credential.ID] = len(descriptors)
			}

			vcFormat := FormatLDPVC
			if credential.JWT != "" {
				vcFormat = FormatJWTVC
			}

			if _, ok := setOfDescriptors[fmt.Sprintf("%s-%s", credential.ID, credential.ID)]; !ok {
				descriptors = append(descriptors, &InputDescriptorMapping{
					ID:     descriptorID,
					Format: presentationFormat,
					Path:   "$",
					PathNested: &InputDescriptorMapping{
						ID:     descriptorID,
						Format: vcFormat,
						Path:   fmt.Sprintf("$.verifiableCredential[%d]", setOfCreds[credential.ID]),
					},
				})
			}
		}
	}

	sort.Sort(byID(descriptors))

	return result, descriptors
}

type byID []*InputDescriptorMapping

func (a byID) Len() int           { return len(a) }
func (a byID) Less(i, j int) bool { return a[i].ID < a[j].ID }
func (a byID) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }

//nolint:funlen,gocyclo
func filterFormat(format *Format, credentials []*verifiable.Credential) (string, []*verifiable.Credential) {
	var ldpCreds, ldpvcCreds, ldpvpCreds, jwtCreds, jwtvcCreds, jwtvpCreds []*verifiable.Credential

	for _, credential := range credentials {
		if credByProof(credential, format.Ldp) {
			ldpCreds = append(ldpCreds, credential)
		}

		if credByProof(credential, format.LdpVC) {
			ldpvcCreds = append(ldpvcCreds, credential)
		}

		if credByProof(credential, format.LdpVP) {
			ldpvpCreds = append(ldpvpCreds, credential)
		}

		var (
			alg    string
			hasAlg bool
		)

		if credential.JWT != "" {
			pJWT, err := jwt.Parse(credential.JWT, jwt.WithSignatureVerifier(&noVerifier{}))
			if err != nil {
				logger.Warnf("unmarshal credential error: %w", err)

				continue
			}

			alg, hasAlg = pJWT.Headers.Algorithm()
		}

		if hasAlg && algMatch(alg, format.Jwt) {
			jwtCreds = append(jwtCreds, credential)
		}

		if hasAlg && algMatch(alg, format.JwtVC) {
			jwtvcCreds = append(jwtvcCreds, credential)
		}

		if hasAlg && algMatch(alg, format.JwtVP) {
			jwtvpCreds = append(jwtvpCreds, credential)
		}
	}

	if len(ldpCreds) > 0 {
		return FormatLDP, ldpCreds
	}

	if len(ldpvcCreds) > 0 {
		return FormatLDPVC, ldpvcCreds
	}

	if len(ldpvpCreds) > 0 {
		return FormatLDPVP, ldpvpCreds
	}

	if len(jwtCreds) > 0 {
		return FormatJWT, jwtCreds
	}

	if len(jwtvcCreds) > 0 {
		return FormatJWTVC, jwtvcCreds
	}

	if len(jwtvpCreds) > 0 {
		return FormatJWTVP, jwtvpCreds
	}

	return "", nil
}

// noVerifier is used when no JWT signature verification is needed.
// To be used with precaution.
type noVerifier struct{}

func (v noVerifier) Verify(_ jose.Headers, _, _, _ []byte) error {
	return nil
}

func algMatch(credAlg string, jwtType *JwtType) bool {
	if jwtType == nil {
		return false
	}

	for _, b := range jwtType.Alg {
		if strings.EqualFold(credAlg, b) {
			return true
		}
	}

	return false
}

func credByProof(c *verifiable.Credential, ldp *LdpType) bool {
	if ldp == nil {
		return false
	}

	for _, proofType := range ldp.ProofType {
		if hasProofWithType(c, proofType) {
			return true
		}
	}

	return false
}

// nolint: gocyclo
func filterSchema(schemas []*Schema, credentials []*verifiable.Credential,
	documentLoader ld.DocumentLoader) []*verifiable.Credential {
	var result []*verifiable.Credential

	contexts := map[string]*ld.Context{}

	for _, credential := range credentials {
		schemaSatisfied := map[string]struct{}{}

		for _, ctx := range credential.Context {
			ctxObj, ok := contexts[ctx]
			if !ok {
				context, err := getContext(ctx, documentLoader)
				if err != nil {
					logger.Errorf("failed to load context '%s': %s", ctx, err.Error())
					return nil
				}

				contexts[ctx] = context
				ctxObj = context
			}

			for _, typ := range credential.Types {
				ids, err := typeFoundInContext(typ, ctxObj)
				if err != nil {
					continue
				}

				for _, id := range ids {
					schemaSatisfied[id] = struct{}{}
				}
			}
		}

		var applicable bool

		for _, schema := range schemas {
			_, ok := schemaSatisfied[schema.URI]
			if ok {
				applicable = true
			} else if schema.Required {
				applicable = false
				break
			}
		}

		if applicable {
			result = append(result, credential)
		}
	}

	return result
}

func typeFoundInContext(typ string, ctxObj *ld.Context) ([]string, error) {
	var out []string

	td := ctxObj.GetTermDefinition(typ)
	if td == nil {
		return nil, nil
	}

	id, ok := td["@id"].(string)
	if ok {
		out = append(out, id)
	}

	tdCtx, ok := td["@context"].(map[string]interface{})
	if !ok {
		return out, nil
	}

	extendedCtx, err := ctxObj.Parse(tdCtx)
	if err != nil {
		return nil, err
	}

	iri, err := extendedCtx.ExpandIri(id, false, false, nil, nil)
	if err != nil {
		return nil, err
	}

	out = append(out, iri)

	return out, nil
}

func getContext(contextURI string, documentLoader ld.DocumentLoader) (*ld.Context, error) {
	contextURI = strings.SplitN(contextURI, "#", 2)[0]

	remoteDoc, err := documentLoader.LoadDocument(contextURI)
	if err != nil {
		return nil, fmt.Errorf("loading document: %w", err)
	}

	doc, ok := remoteDoc.Document.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("expects jsonld document to be unmarshaled into map[string]interface{}")
	}

	ctx, ok := doc["@context"]
	if !ok {
		return nil, fmt.Errorf("@context field not found in context %s", contextURI)
	}

	activeCtx, err := ld.NewContext(nil, nil).Parse(ctx)
	if err != nil {
		return nil, err
	}

	return activeCtx, nil
}
