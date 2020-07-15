Helm Chart for Tuenti Secrets-Manager
=====================================

This chart is attempting to simplify installation of the Tuenti Secrets-Manager kubernetes operator.  It currently supports the core features but not yet all command-line options.

This chart can be used in two ways:
   1) Deployed in the ClusterScope; that is, it can monitor all namespaces for secretdefinition CRs;
   2) Deployed in a NamespaceScope in which it can only monitor a specific namespace for secretdefinition CRs.

In the latter case, it cannot manage multiple namespace.  To accomplish this it is recommended to have multiple deployments in each of the desired namespaces.  Additionally, it is recommended that only the minimal access is given to the AppRoles for each namespaced deployment.  One approach that should work is to namespace your projects and have a corresponding secrets path in Vault for that project: then have a deployment of this chart to that project namespace use an AppRole and policy that only grants access to the corresponding Vault secrets path for the project.

See the `values.yaml` file for details and options on how to use this chart.

Chart Settings:
 - Note that the image tag will default to use the Chart `appVersion` which should match the version of the `tuentitech/secrets-manager`  Docker image on Docker Hub.  This can be overriden by setting the `image.tag` setting in the values.yaml file.  This version should never include the `v` prefix -- it will be added automatically so it matches the image tag on Docker Hub.
 - The kubernetes **SeviceAccount** will be created automatically unless disabled.  If disabled, you should provide the name of the externally managed ServiceAccount to be used (`serviceAccount.name`).
 - Similarly, the **AppRole Secret** used to manage the Vault AppRole *role_id* and *secret_id* will be created by default using values provided in the `values.yaml` file.  The values should be plain-text -- the cart will convert them to base64 encoding in the generated Secret.  You can disable creation of the Secret and manage that secret separately but you should provide the Secret name in the `appRoleSecret.name` setting.
 - If you let Helm manage your *AppRole Secret* it is a good practice to put this sensitive information in an encrypted settings file.  The **Helm-Secrets** Helm plugin provides an excellent way to manage sensitive settings for Helm:  https://github.com/zendesk/helm-secrets
 - For everything except local testing, it is recommended that you have Vault set up with an HTTPS (TLS) endpoint.
 - For the `vaultUrl` setting it is recommended to either access it by the service name if this chart is installed to the same namespace as is Vault; e.g.: `https://vault:8200`, otherwise if Vault is installed to a different namespace it is recommended to use the fully-qualified domain name to reduce DNS latency; e.g.: (if Vault is in the *default* namespace) `https://vault.default.svc.cluster.local.:8200` -- this is following the *Five-Dots Rule* (https://pracucci.com/kubernetes-dns-resolution-ndots-options-and-why-it-may-affect-application-performances.html).
 - Setting `debugEnabled` to `true` will add the `-enable-debug-log` option.  This is turned off by default.
 - RBAC settings for a _ClusterRole_ and _ClusterRoleBinding_ are automatically created only if the _ServiceAccount_ is also managed by this Chart.
 - Namespaced _Role_ and _RoleBinding_ RBAC resources are created instead of ClusterRole and ClusterRoleBindings, if the **namespaceScopeEnabled** is set to *true*.
 - Non-test Vault instances should have TLS enabled, but this Chart does not manage the associated Secret in kubernetes.  Instead you should add the appropriate settings under the `vaultTLS` section of the values.yaml file.  At a minimum, this secret should include the CA certificate file in PEM format so it can be mounted in the Pod and used for access to Vault with TLS (the chart will add the **VAULT_CACERT** environment variable which the Secrets-Manager fortunately supports).
 - This chart includes suport for *PodSecurityContext* and *SecurityContext* but these have not yet been tested.  We don't know if the Docker image for Secrets-Manager will work these (hopefully they will just work without changes).
 - Standard support for *resource* **requests** and **limits** are supported by this chart. (https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/)
 - You can control which cluster nodes this chart's Pod get scheduled to using one of (or a combination of) *NodeSelectors*, *Affinity* and *AntiAffinity* rules, as well as *Taints and Tolerations*.
 - This chart doesn't support the use of ConfigMaps for SecretDefinitions.  You must use a CR instance of the provided CRD.

 This Chart is compatible with Helm v3.  Helm v3 uses a different approach for managing CRDs so if you are not using Helm v3 you should manually install them from the GitHub repo https://github.com/tuenti/secrets-manager.

 Installation
 ------------

1. Copy the values.yaml file from the chart:
   `cp charts/secrets-manager/values.yaml my-project-values.yaml`
2. Edit your values.yaml file with settings for your deployment.  In particular, pay attention to the `namespaceScopeEnabled` to isolate the activity of your Secrets-Manager deployment to one namespace.  While the Cluster-Scoped deployment may be easier to set up, A set of Namespace-Scoped deployments will give you finer grained control over your Vault secrets.
3. Add the Vault Policy and AppRole for your Secrets-Manager instance -- ideally this should only grant access to the minimal scope required.  For example, in a Namespace-Scoped deployment you might want the project (or team) associated with that Namespace to only have access to a Vault Path set up for that project (or team):
   ```
   (
      cat <<EOF
   path "secret/data/my-project/*" {
   capabilities = ["read"]
   }
   EOF
   ) | vault policy write my-project-policy -
   vault write auth/approle/role/my-project-secrets-manager policies=my-project-policy secret_id_num_uses=0 secret_id_ttl=0
   ```
4. Retrieve the AppRole *role_id* and *secret_id* (this uses *jq* to parse out the values we need: https://stedolan.github.io/jq/):
   ```
   export PROJECT_SECRET_ID=$(vault write -force auth/approle/role/my-project-secrets-manager/secret-id -format=json | jq '.data.secret_id')
   export PROJECT_ROLE_ID=$(vault read auth/approle/role/my-project-secrets-manager/role-id -format=json | jq '.data.role_id')
   ```
5. Although the Chart supports generating the AppRole Secret from settings in the values.yaml file, it is more secure to manually create the Secret and reference it by name in the values.yaml file.  For example in a namespace-scoped deployment wih Namespace *my-project* (using the variables set in the previous step):
   ```
   kubectl create secret generic my-project-approle-secret --from-literal role_id=$PROJECT_ROLE_ID  --from-literal secret_id=$PROJECT_SECRET_ID --namespace my-project
   ```
and add this secret name to your values.yaml file under `appRoleSecret.name`.  Note that if you are doing this you must set `appRoleSecret.create` to `false` so the Helm deployment does not overwrite the secret your just created.  Alternatively, you can set your `role_id` and `secret_id` in your values.yaml file and have Helm manage the secret, though when you check in your values.yaml file this will potentially expose these values and weaken security.

6. Install the Chart using your completed values.yaml file.  For example:
   ```
   helm install my-project-secrets-manager charts/secrets-manager --namespace my-project --values my-project-values.yaml
   ```
  **Do not** install multiple instances of Secrets-Manager to the same Namespace.  This will cause conflicts.

7. Create and apply SecretDefinition resources to your project Namespace and the operator will create (or update) Secrets with values from Vault.

See the ReadMe for the Secrets-Manager repo for more details on using SecretDefinitions:  https://github.com/tuenti/secrets-manager

To-Do
-----
A number of features are not yet supported by this Helm chart but may be added in the future, as needed:
 - Will we need to suuport *enable-leader-election*?  Can Secrets-Manager sun with `replicas` set to more than 1 (HA configuration)?
 - `controller-name` setting appears to be unsupported in v1.0.2 of Secrets-Manager.  This is ilkely not needed in Namespace-Scoped deployments unless we need to have multiple Secrets-Manager instances in the same Namespace or to avoid conflict between Cluster-Scoped and Namespace-Scoped deployments.  Perhaps the *exclude-namespaces* setting will be enough to avoid conflicts.

 Note that some Helm templating features do not handle hyphen ("-") characters in settings names so the settings in values.yaml removes the hyphens but uses (camelCase) inter-capitalisation to preserve the words (e.g. `approle-path` becomes the setting `approlePath` in the values.yaml file).