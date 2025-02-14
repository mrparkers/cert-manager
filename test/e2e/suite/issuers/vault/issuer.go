/*
Copyright 2019 The Jetstack cert-manager contributors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package vault

import (
	"path"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/jetstack/cert-manager/pkg/apis/certmanager/v1alpha2"
	cmmeta "github.com/jetstack/cert-manager/pkg/apis/meta/v1"
	"github.com/jetstack/cert-manager/test/e2e/framework"
	"github.com/jetstack/cert-manager/test/e2e/framework/addon/tiller"
	vaultaddon "github.com/jetstack/cert-manager/test/e2e/framework/addon/vault"
	"github.com/jetstack/cert-manager/test/e2e/util"
)

var _ = framework.CertManagerDescribe("Vault Issuer", func() {
	f := framework.NewDefaultFramework("create-vault-issuer")

	var (
		tiller = &tiller.Tiller{
			Name:               "tiller-deploy",
			ClusterPermissions: false,
		}
		vault = &vaultaddon.Vault{
			Tiller: tiller,
			Name:   "cm-e2e-create-vault-issuer",
		}
	)

	BeforeEach(func() {
		tiller.Namespace = f.Namespace.Name
		vault.Namespace = f.Namespace.Name
	})

	f.RequireAddon(tiller)
	f.RequireAddon(vault)

	issuerName := "test-vault-issuer"
	rootMount := "root-ca"
	intermediateMount := "intermediate-ca"
	role := "kubernetes-vault"
	vaultSecretAppRoleName := "vault-role"
	vaultSecretTokenName := "vault-token"
	vaultSecretServiceAccount := "vault-serviceaccount"
	vaultKubernetesRoleName := "kubernetes-role"
	vaultPath := path.Join(intermediateMount, "sign", role)
	appRoleAuthPath := "approle"
	kubernetesAuthPath := "kubernetes"
	var roleId, secretId string
	var vaultInit *vaultaddon.VaultInitializer

	BeforeEach(func() {
		By("Configuring the Vault server")

		apiHost := "https://kubernetes.default.svc.cluster.local" // since vault is running in-cluster
		caCert := string(f.KubeClientConfig.CAData)

		Expect(apiHost).NotTo(BeEmpty())
		Expect(caCert).NotTo(BeEmpty())

		vaultInit = &vaultaddon.VaultInitializer{
			Details:           *vault.Details(),
			RootMount:         rootMount,
			IntermediateMount: intermediateMount,
			Role:              role,
			AppRoleAuthPath:   appRoleAuthPath,
			APIServerURL:      apiHost,
			APIServerCA:       caCert,
		}

		err := vaultInit.Init()
		Expect(err).NotTo(HaveOccurred())
		err = vaultInit.Setup()
		Expect(err).NotTo(HaveOccurred())
		roleId, secretId, err = vaultInit.CreateAppRole()
		Expect(err).NotTo(HaveOccurred())

		By("creating a service account for Vault authentication")
		err = vaultInit.CreateKubernetesRole(f.KubeClientSet, f.Namespace.Name, vaultKubernetesRoleName, vaultSecretServiceAccount)
		Expect(err).NotTo(HaveOccurred())
	})

	JustAfterEach(func() {
		By("Cleaning up AppRole")
		f.CertManagerClientSet.CertmanagerV1alpha2().Issuers(f.Namespace.Name).Delete(issuerName, nil)
		f.KubeClientSet.CoreV1().Secrets(f.Namespace.Name).Delete(vaultSecretAppRoleName, nil)
		vaultInit.CleanAppRole()

		By("Cleaning up Kubernetes")
		vaultInit.CleanKubernetesRole(f.KubeClientSet, f.Namespace.Name, vaultKubernetesRoleName, vaultSecretServiceAccount)

		By("Cleaning up Vault")
		Expect(vaultInit.Clean()).NotTo(HaveOccurred())
	})

	const vaultDefaultDuration = time.Hour * 24 * 90

	It("should be ready with a valid AppRole", func() {
		_, err := f.KubeClientSet.CoreV1().Secrets(f.Namespace.Name).Create(vaultaddon.NewVaultAppRoleSecret(vaultSecretAppRoleName, secretId))
		Expect(err).NotTo(HaveOccurred())

		_, err = f.CertManagerClientSet.CertmanagerV1alpha2().Issuers(f.Namespace.Name).Create(util.NewCertManagerVaultIssuerAppRole(issuerName, vault.Details().Host, vaultPath, roleId, vaultSecretAppRoleName, appRoleAuthPath, vault.Details().VaultCA))
		Expect(err).NotTo(HaveOccurred())

		By("Waiting for Issuer to become Ready")
		err = util.WaitForIssuerCondition(f.CertManagerClientSet.CertmanagerV1alpha2().Issuers(f.Namespace.Name),
			issuerName,
			v1alpha2.IssuerCondition{
				Type:   v1alpha2.IssuerConditionReady,
				Status: cmmeta.ConditionTrue,
			})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should fail to init with missing Vault AppRole", func() {
		By("Creating an Issuer")
		_, err := f.CertManagerClientSet.CertmanagerV1alpha2().Issuers(f.Namespace.Name).Create(util.NewCertManagerVaultIssuerAppRole(issuerName, vault.Details().Host, vaultPath, roleId, vaultSecretAppRoleName, appRoleAuthPath, vault.Details().VaultCA))
		Expect(err).NotTo(HaveOccurred())

		By("Waiting for Issuer to become Ready")
		err = util.WaitForIssuerCondition(f.CertManagerClientSet.CertmanagerV1alpha2().Issuers(f.Namespace.Name),
			issuerName,
			v1alpha2.IssuerCondition{
				Type:   v1alpha2.IssuerConditionReady,
				Status: cmmeta.ConditionFalse,
			})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should fail to init with missing Vault Token", func() {
		By("Creating an Issuer")
		_, err := f.CertManagerClientSet.CertmanagerV1alpha2().Issuers(f.Namespace.Name).Create(util.NewCertManagerVaultIssuerToken(issuerName, vault.Details().Host, vaultPath, vaultSecretTokenName, appRoleAuthPath, vault.Details().VaultCA))
		Expect(err).NotTo(HaveOccurred())

		By("Waiting for Issuer to become Ready")
		err = util.WaitForIssuerCondition(f.CertManagerClientSet.CertmanagerV1alpha2().Issuers(f.Namespace.Name),
			issuerName,
			v1alpha2.IssuerCondition{
				Type:   v1alpha2.IssuerConditionReady,
				Status: cmmeta.ConditionFalse,
			})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should be ready with a valid Kubernetes Role and ServiceAccount Secret", func() {
		_, err := f.KubeClientSet.CoreV1().Secrets(f.Namespace.Name).Create(vaultaddon.NewVaultKubernetesSecret(vaultSecretServiceAccount, vaultSecretServiceAccount))
		Expect(err).NotTo(HaveOccurred())

		_, err = f.CertManagerClientSet.CertmanagerV1alpha2().Issuers(f.Namespace.Name).Create(util.NewCertManagerVaultIssuerKubernetes(issuerName, vault.Details().Host, vaultPath, vaultSecretServiceAccount, vaultKubernetesRoleName, kubernetesAuthPath, vault.Details().VaultCA))
		Expect(err).NotTo(HaveOccurred())

		By("Waiting for Issuer to become Ready")
		err = util.WaitForIssuerCondition(f.CertManagerClientSet.CertmanagerV1alpha2().Issuers(f.Namespace.Name),
			issuerName,
			v1alpha2.IssuerCondition{
				Type:   v1alpha2.IssuerConditionReady,
				Status: cmmeta.ConditionTrue,
			})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should fail to init with missing Kubernetes Role", func() {
		By("Creating an Issuer")
		_, err := f.CertManagerClientSet.CertmanagerV1alpha2().Issuers(f.Namespace.Name).Create(util.NewCertManagerVaultIssuerKubernetes(issuerName, vault.Details().Host, vaultPath, vaultSecretServiceAccount, vaultKubernetesRoleName, kubernetesAuthPath, vault.Details().VaultCA))
		Expect(err).NotTo(HaveOccurred())
		By("Waiting for Issuer to become Ready")
		err = util.WaitForIssuerCondition(f.CertManagerClientSet.CertmanagerV1alpha2().Issuers(f.Namespace.Name),
			issuerName,
			v1alpha2.IssuerCondition{
				Type:   v1alpha2.IssuerConditionReady,
				Status: cmmeta.ConditionFalse,
			})
		Expect(err).NotTo(HaveOccurred())
	})
})
