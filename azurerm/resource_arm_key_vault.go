package azurerm

import (
	"fmt"
	"log"
	"regexp"

	"time"

	"net"

	"github.com/Azure/azure-sdk-for-go/arm/keyvault"
	"github.com/hashicorp/go-getter/helper/url"
	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"
	"github.com/hashicorp/terraform/helper/validation"
	"github.com/satori/uuid"
	"github.com/terraform-providers/terraform-provider-azurerm/azurerm/utils"
)

// As can be seen in the API definition, the Sku Family only supports the value
// `A` and is a required field
// https://github.com/Azure/azure-rest-api-specs/blob/master/arm-keyvault/2015-06-01/swagger/keyvault.json#L239
var armKeyVaultSkuFamily = "A"

func resourceArmKeyVault() *schema.Resource {
	return &schema.Resource{
		Create: resourceArmKeyVaultCreate,
		Read:   resourceArmKeyVaultRead,
		Update: resourceArmKeyVaultCreate,
		Delete: resourceArmKeyVaultDelete,
		Importer: &schema.ResourceImporter{
			State: schema.ImportStatePassthrough,
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:         schema.TypeString,
				Required:     true,
				ForceNew:     true,
				ValidateFunc: validateKeyVaultName,
			},

			"location": locationSchema(),

			"resource_group_name": resourceGroupNameSchema(),

			"sku": {
				Type:     schema.TypeSet,
				Required: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"name": {
							Type:     schema.TypeString,
							Required: true,
							ValidateFunc: validation.StringInSlice([]string{
								string(keyvault.Standard),
								string(keyvault.Premium),
							}, false),
						},
					},
				},
			},

			"vault_uri": {
				Type:     schema.TypeString,
				Computed: true,
			},

			"tenant_id": {
				Type:         schema.TypeString,
				Required:     true,
				ValidateFunc: validateUUID,
			},

			"access_policy": {
				Type:     schema.TypeList,
				Optional: true,
				MinItems: 1,
				MaxItems: 16,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"tenant_id": {
							Type:         schema.TypeString,
							Required:     true,
							ValidateFunc: validateUUID,
						},
						"object_id": {
							Type:         schema.TypeString,
							Required:     true,
							ValidateFunc: validateUUID,
						},
						"application_id": {
							Type:         schema.TypeString,
							Optional:     true,
							ValidateFunc: validateUUID,
						},
						"certificate_permissions": {
							Type:     schema.TypeList,
							Optional: true,
							Elem: &schema.Schema{
								Type: schema.TypeString,
								ValidateFunc: validation.StringInSlice([]string{
									string(keyvault.All),
									string(keyvault.Create),
									string(keyvault.Delete),
									string(keyvault.Deleteissuers),
									string(keyvault.Get),
									string(keyvault.Getissuers),
									string(keyvault.Import),
									string(keyvault.List),
									string(keyvault.Listissuers),
									string(keyvault.Managecontacts),
									string(keyvault.Manageissuers),
									string(keyvault.Setissuers),
									string(keyvault.Update),
								}, true),
								DiffSuppressFunc: ignoreCaseDiffSuppressFunc,
							},
						},
						"key_permissions": {
							Type:     schema.TypeList,
							Required: true,
							Elem: &schema.Schema{
								Type: schema.TypeString,
								ValidateFunc: validation.StringInSlice([]string{
									string(keyvault.KeyPermissionsAll),
									string(keyvault.KeyPermissionsBackup),
									string(keyvault.KeyPermissionsCreate),
									string(keyvault.KeyPermissionsDecrypt),
									string(keyvault.KeyPermissionsDelete),
									string(keyvault.KeyPermissionsEncrypt),
									string(keyvault.KeyPermissionsGet),
									string(keyvault.KeyPermissionsImport),
									string(keyvault.KeyPermissionsList),
									string(keyvault.KeyPermissionsRestore),
									string(keyvault.KeyPermissionsSign),
									string(keyvault.KeyPermissionsUnwrapKey),
									string(keyvault.KeyPermissionsUpdate),
									string(keyvault.KeyPermissionsVerify),
									string(keyvault.KeyPermissionsWrapKey),
								}, true),
								DiffSuppressFunc: ignoreCaseDiffSuppressFunc,
							},
						},
						"secret_permissions": {
							Type:     schema.TypeList,
							Required: true,
							Elem: &schema.Schema{
								Type: schema.TypeString,
								ValidateFunc: validation.StringInSlice([]string{
									string(keyvault.SecretPermissionsAll),
									string(keyvault.SecretPermissionsDelete),
									string(keyvault.SecretPermissionsGet),
									string(keyvault.SecretPermissionsList),
									string(keyvault.SecretPermissionsSet),
								}, true),
								DiffSuppressFunc: ignoreCaseDiffSuppressFunc,
							},
						},
					},
				},
			},

			"enabled_for_deployment": {
				Type:     schema.TypeBool,
				Optional: true,
			},

			"enabled_for_disk_encryption": {
				Type:     schema.TypeBool,
				Optional: true,
			},

			"enabled_for_template_deployment": {
				Type:     schema.TypeBool,
				Optional: true,
			},

			"tags": tagsSchema(),
		},
	}
}

func resourceArmKeyVaultCreate(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).keyVaultClient
	log.Printf("[INFO] preparing arguments for Azure ARM KeyVault creation.")

	name := d.Get("name").(string)
	location := d.Get("location").(string)
	resGroup := d.Get("resource_group_name").(string)
	tenantUUID := uuid.FromStringOrNil(d.Get("tenant_id").(string))
	enabledForDeployment := d.Get("enabled_for_deployment").(bool)
	enabledForDiskEncryption := d.Get("enabled_for_disk_encryption").(bool)
	enabledForTemplateDeployment := d.Get("enabled_for_template_deployment").(bool)
	tags := d.Get("tags").(map[string]interface{})

	parameters := keyvault.VaultCreateOrUpdateParameters{
		Location: &location,
		Properties: &keyvault.VaultProperties{
			TenantID:                     &tenantUUID,
			Sku:                          expandKeyVaultSku(d),
			AccessPolicies:               expandKeyVaultAccessPolicies(d),
			EnabledForDeployment:         &enabledForDeployment,
			EnabledForDiskEncryption:     &enabledForDiskEncryption,
			EnabledForTemplateDeployment: &enabledForTemplateDeployment,
		},
		Tags: expandTags(tags),
	}

	_, err := client.CreateOrUpdate(resGroup, name, parameters)
	if err != nil {
		return err
	}

	read, err := client.Get(resGroup, name)
	if err != nil {
		return err
	}
	if read.ID == nil {
		return fmt.Errorf("Cannot read KeyVault %s (resource group %s) ID", name, resGroup)
	}

	d.SetId(*read.ID)

	if d.IsNewResource() {
		if props := read.Properties; props != nil {
			if vault := props.VaultURI; vault != nil {
				err := resource.Retry(30*time.Second, checkKeyVaultDNSIsAvailable(*vault))
				if err != nil {
					return err
				}
			}
		}
	}

	return resourceArmKeyVaultRead(d, meta)
}

func resourceArmKeyVaultRead(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).keyVaultClient

	id, err := parseAzureResourceID(d.Id())
	if err != nil {
		return err
	}
	resGroup := id.ResourceGroup
	name := id.Path["vaults"]

	resp, err := client.Get(resGroup, name)
	if err != nil {
		if utils.ResponseWasNotFound(resp.Response) {
			d.SetId("")
			return nil
		}
		return fmt.Errorf("Error making Read request on Azure KeyVault %s: %+v", name, err)
	}

	d.Set("name", resp.Name)
	d.Set("resource_group_name", resGroup)
	d.Set("location", azureRMNormalizeLocation(*resp.Location))
	d.Set("tenant_id", resp.Properties.TenantID.String())
	d.Set("enabled_for_deployment", resp.Properties.EnabledForDeployment)
	d.Set("enabled_for_disk_encryption", resp.Properties.EnabledForDiskEncryption)
	d.Set("enabled_for_template_deployment", resp.Properties.EnabledForTemplateDeployment)
	d.Set("sku", flattenKeyVaultSku(resp.Properties.Sku))
	d.Set("access_policy", flattenKeyVaultAccessPolicies(resp.Properties.AccessPolicies))
	d.Set("vault_uri", resp.Properties.VaultURI)

	flattenAndSetTags(d, resp.Tags)

	return nil
}

func resourceArmKeyVaultDelete(d *schema.ResourceData, meta interface{}) error {
	client := meta.(*ArmClient).keyVaultClient

	id, err := parseAzureResourceID(d.Id())
	if err != nil {
		return err
	}
	resGroup := id.ResourceGroup
	name := id.Path["vaults"]

	_, err = client.Delete(resGroup, name)

	return err
}

func expandKeyVaultSku(d *schema.ResourceData) *keyvault.Sku {
	skuSets := d.Get("sku").(*schema.Set).List()
	sku := skuSets[0].(map[string]interface{})

	return &keyvault.Sku{
		Family: &armKeyVaultSkuFamily,
		Name:   keyvault.SkuName(sku["name"].(string)),
	}
}

func expandKeyVaultAccessPolicies(d *schema.ResourceData) *[]keyvault.AccessPolicyEntry {
	policies := d.Get("access_policy").([]interface{})
	result := make([]keyvault.AccessPolicyEntry, 0, len(policies))

	for _, policySet := range policies {
		policyRaw := policySet.(map[string]interface{})

		certificatePermissionsRaw := policyRaw["certificate_permissions"].([]interface{})
		certificatePermissions := []keyvault.CertificatePermissions{}
		for _, permission := range certificatePermissionsRaw {
			certificatePermissions = append(certificatePermissions, keyvault.CertificatePermissions(permission.(string)))
		}

		keyPermissionsRaw := policyRaw["key_permissions"].([]interface{})
		keyPermissions := []keyvault.KeyPermissions{}
		for _, permission := range keyPermissionsRaw {
			keyPermissions = append(keyPermissions, keyvault.KeyPermissions(permission.(string)))
		}

		secretPermissionsRaw := policyRaw["secret_permissions"].([]interface{})
		secretPermissions := []keyvault.SecretPermissions{}
		for _, permission := range secretPermissionsRaw {
			secretPermissions = append(secretPermissions, keyvault.SecretPermissions(permission.(string)))
		}

		policy := keyvault.AccessPolicyEntry{
			Permissions: &keyvault.Permissions{
				Certificates: &certificatePermissions,
				Keys:         &keyPermissions,
				Secrets:      &secretPermissions,
			},
		}

		tenantUUID := uuid.FromStringOrNil(policyRaw["tenant_id"].(string))
		policy.TenantID = &tenantUUID
		objectUUID := policyRaw["object_id"].(string)
		policy.ObjectID = &objectUUID

		if v := policyRaw["application_id"]; v != "" {
			applicationUUID := uuid.FromStringOrNil(v.(string))
			policy.ApplicationID = &applicationUUID
		}

		result = append(result, policy)
	}

	return &result
}

func flattenKeyVaultSku(sku *keyvault.Sku) []interface{} {
	result := map[string]interface{}{
		"name": string(sku.Name),
	}

	return []interface{}{result}
}

func flattenKeyVaultAccessPolicies(policies *[]keyvault.AccessPolicyEntry) []interface{} {
	result := make([]interface{}, 0, len(*policies))

	for _, policy := range *policies {
		policyRaw := make(map[string]interface{})

		keyPermissionsRaw := make([]interface{}, 0, len(*policy.Permissions.Keys))
		for _, keyPermission := range *policy.Permissions.Keys {
			keyPermissionsRaw = append(keyPermissionsRaw, string(keyPermission))
		}

		secretPermissionsRaw := make([]interface{}, 0, len(*policy.Permissions.Secrets))
		for _, secretPermission := range *policy.Permissions.Secrets {
			secretPermissionsRaw = append(secretPermissionsRaw, string(secretPermission))
		}

		policyRaw["tenant_id"] = policy.TenantID.String()
		policyRaw["object_id"] = *policy.ObjectID
		if policy.ApplicationID != nil {
			policyRaw["application_id"] = policy.ApplicationID.String()
		}
		policyRaw["key_permissions"] = keyPermissionsRaw
		policyRaw["secret_permissions"] = secretPermissionsRaw

		if policy.Permissions.Certificates != nil {
			certificatePermissionsRaw := make([]interface{}, 0, len(*policy.Permissions.Certificates))
			for _, certificatePermission := range *policy.Permissions.Certificates {
				certificatePermissionsRaw = append(certificatePermissionsRaw, string(certificatePermission))
			}
			policyRaw["certificate_permissions"] = certificatePermissionsRaw
		}

		result = append(result, policyRaw)
	}

	return result
}

func validateKeyVaultName(v interface{}, k string) (ws []string, errors []error) {
	value := v.(string)
	if matched := regexp.MustCompile(`^[a-zA-Z0-9-]{3,24}$`).Match([]byte(value)); !matched {
		errors = append(errors, fmt.Errorf("%q may only contain alphanumeric characters and dashes and must be between 3-24 chars", k))
	}

	return
}

func checkKeyVaultDNSIsAvailable(vaultUri string) func() *resource.RetryError {
	return func() *resource.RetryError {
		uri, err := url.Parse(vaultUri)
		if err != nil {
			return resource.NonRetryableError(err)
		}

		conn, err := net.Dial("tcp", fmt.Sprintf("%s:443", uri.Host))
		if err != nil {
			return resource.RetryableError(err)
		}

		_ = conn.Close()
		return nil
	}
}
