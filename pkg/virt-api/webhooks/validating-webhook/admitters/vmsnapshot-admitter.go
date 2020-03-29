/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright 2018 Red Hat, Inc.
 *
 */

package admitters

import (
	"encoding/json"
	"fmt"
	"reflect"

	"k8s.io/api/admission/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfield "k8s.io/apimachinery/pkg/util/validation/field"

	vmsnapshotv1alpha1 "kubevirt.io/client-go/apis/snapshot/v1alpha1"
	"kubevirt.io/client-go/kubecli"
	webhookutils "kubevirt.io/kubevirt/pkg/util/webhooks"
)

// VMSnapshotAdmitter validates VirtualMachineSnapshots
type VMSnapshotAdmitter struct {
	Client kubecli.KubevirtClient
}

// NewVMSnapshotAdmitter creates a VMSnapshotAdmitter
func NewVMSnapshotAdmitter(client kubecli.KubevirtClient) *VMSnapshotAdmitter {
	return &VMSnapshotAdmitter{
		Client: client,
	}
}

// Admit validates an AdmissionReview
func (admitter *VMSnapshotAdmitter) Admit(ar *v1beta1.AdmissionReview) *v1beta1.AdmissionResponse {
	if ar.Request.Resource.Group != vmsnapshotv1alpha1.SchemeGroupVersion.Group ||
		ar.Request.Resource.Resource != "virtualmachinesnapshots" {
		return webhookutils.ToAdmissionResponseError(fmt.Errorf("Unexpected Resource %+v", ar.Request.Resource))
	}

	vmSnapshot := &vmsnapshotv1alpha1.VirtualMachineSnapshot{}
	// TODO ideally use UniversalDeserializer here
	err := json.Unmarshal(ar.Request.Object.Raw, vmSnapshot)
	if err != nil {
		return webhookutils.ToAdmissionResponseError(err)
	}

	var causes []metav1.StatusCause

	switch ar.Request.Operation {
	case v1beta1.Create:
		sourceField := k8sfield.NewPath("spec", "source")

		switch {
		case vmSnapshot.Spec.Source.VirtualMachineName != nil:
			causes, err = admitter.validateCreateVM(sourceField.Child("virtualMachineName"), ar.Request.Namespace, *vmSnapshot.Spec.Source.VirtualMachineName)
			if err != nil {
				return webhookutils.ToAdmissionResponseError(err)
			}
		default:
			causes = []metav1.StatusCause{
				{
					Type:    metav1.CauseTypeFieldValueNotFound,
					Message: "missing source name",
					Field:   sourceField.String(),
				},
			}
		}
	case v1beta1.Update:
		prevObj := &vmsnapshotv1alpha1.VirtualMachineSnapshot{}
		err = json.Unmarshal(ar.Request.OldObject.Raw, prevObj)
		if err != nil {
			return webhookutils.ToAdmissionResponseError(err)
		}

		if !reflect.DeepEqual(prevObj.Spec, vmSnapshot.Spec) {
			causes = []metav1.StatusCause{
				{
					Type:    metav1.CauseTypeFieldValueInvalid,
					Message: "spec in immutable after creation",
					Field:   k8sfield.NewPath("spec").String(),
				},
			}
		}
	default:
		return webhookutils.ToAdmissionResponseError(fmt.Errorf("unexpected operation %s", ar.Request.Operation))
	}

	if len(causes) > 0 {
		return webhookutils.ToAdmissionResponse(causes)
	}

	reviewResponse := v1beta1.AdmissionResponse{
		Allowed: true,
	}
	return &reviewResponse
}

func (admitter *VMSnapshotAdmitter) validateCreateVM(field *k8sfield.Path, namespace, name string) ([]metav1.StatusCause, error) {
	vm, err := admitter.Client.VirtualMachine(namespace).Get(name, &metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return []metav1.StatusCause{
			{
				Type:    metav1.CauseTypeFieldValueInvalid,
				Message: fmt.Sprintf("VirtualMachine %q does not exist", name),
				Field:   field.String(),
			},
		}, nil
	}

	if err != nil {
		return nil, err
	}

	var causes []metav1.StatusCause

	if vm.Spec.Running != nil && *vm.Spec.Running {
		cause := metav1.StatusCause{
			Type:    metav1.CauseTypeFieldValueInvalid,
			Message: fmt.Sprintf("VirtualMachine %q is running", name),
			Field:   field.String(),
		}
		causes = append(causes, cause)
	}

	return causes, nil
}
