<template>
  <div class="tenant-orgs">
    <div class="section-header">
      <div class="section-header-row">
        <div class="section-header-titlewrap">
          <h2>{{ $t('tenantOrg.title') }}</h2>
        </div>
        <t-button v-if="canManageTenant" theme="primary" size="small" @click="openCreateDialog">
          <template #icon><t-icon name="add" /></template>
          {{ $t('tenantOrg.create') }}
        </t-button>
      </div>
      <p class="section-description">{{ $t('tenantOrg.sectionDescription') }}</p>
    </div>

    <t-loading :loading="loading">
      <t-table row-key="id" :data="orgs" :columns="orgColumns" size="medium" hover>
        <template #name="{ row }">
          <div class="org-name-cell">
            <span class="org-name">{{ row.name }}</span>
            <span class="org-member-count">{{ $t('tenantOrg.memberCount', { count: row.member_count || 0 }) }}</span>
          </div>
        </template>
        <template #actions="{ row }">
          <t-button variant="text" size="small" @click="openMembers(row)">
            {{ $t('tenantOrg.members') }}
          </t-button>
          <t-button variant="text" size="small" @click="openInvite(row)">
            {{ $t('tenantOrg.inviteLink') }}
          </t-button>
          <t-button v-if="canManageTenant || managedOrgIds.has(row.id)" variant="text" size="small" @click="openEdit(row)">
            {{ $t('common.edit') }}
          </t-button>
          <t-button v-if="canManageTenant" theme="danger" variant="text" size="small" @click="confirmDelete(row)">
            {{ $t('common.delete') }}
          </t-button>
        </template>
        <template #empty>
          <div class="empty-state">{{ $t('tenantOrg.empty') }}</div>
        </template>
      </t-table>
    </t-loading>

    <!-- Create / edit org -->
    <t-dialog v-model:visible="orgDialog" :header="editingOrg ? $t('tenantOrg.editTitle') : $t('tenantOrg.createTitle')"
      :confirm-btn="{ content: $t('common.confirm'), loading: saving }" :on-confirm="saveOrg"
      :on-close="() => (orgDialog = false)">
      <t-form label-align="top">
        <t-form-item :label="$t('tenantOrg.nameLabel')" required>
          <t-input v-model="orgForm.name" :maxlength="255" :placeholder="$t('tenantOrg.namePlaceholder')" />
        </t-form-item>
        <t-form-item :label="$t('tenantOrg.descriptionLabel')">
          <t-textarea v-model="orgForm.description" :maxlength="1000" :autosize="{ minRows: 2, maxRows: 4 }" />
        </t-form-item>
      </t-form>
    </t-dialog>

    <!-- Members drawer -->
    <t-drawer v-model:visible="membersDrawer" :header="membersTitle" size="480px">
      <div class="members-pane">
        <div class="members-toolbar">
          <t-select v-model="newMemberId" :options="memberCandidates" filterable
            :placeholder="$t('tenantOrg.addMemberPlaceholder')" class="member-select" />
          <t-select v-model="newMemberRole" :options="orgRoleOptions" class="role-select" />
          <t-button theme="primary" size="small" :disabled="!newMemberId" :loading="saving" @click="addMember">
            {{ $t('tenantOrg.addMember') }}
          </t-button>
        </div>
        <t-table row-key="user_id" :data="members" :columns="memberColumns" size="small" hover>
          <template #role="{ row }">
            <t-tag :theme="row.role === 'manager' ? 'primary' : 'default'" variant="light" size="small">
              {{ $t('tenantOrg.role.' + row.role) }}
            </t-tag>
          </template>
          <template #memberActions="{ row }">
            <t-button v-if="row.role === 'member'" variant="text" size="small" @click="promote(row)">
              {{ $t('tenantOrg.makeManager') }}
            </t-button>
            <t-button v-else variant="text" size="small" @click="demote(row)">
              {{ $t('tenantOrg.makeMember') }}
            </t-button>
            <t-button theme="danger" variant="text" size="small" @click="removeMember(row)">
              {{ $t('common.delete') }}
            </t-button>
          </template>
        </t-table>
      </div>
    </t-drawer>

    <!-- Invite link dialog -->
    <t-dialog v-model:visible="inviteDialog" :header="$t('tenantOrg.inviteTitle', { org: activeOrg?.name || '' })"
      :confirm-btn="{ content: $t('tenantOrg.generateLink'), loading: saving }" :on-confirm="generateInvite"
      :on-close="() => (inviteDialog = false)">
      <t-form label-align="top">
        <t-form-item :label="$t('tenantOrg.inviteRole')">
          <t-select v-model="inviteRole" :options="inviteRoleOptions" />
        </t-form-item>
        <t-form-item :label="$t('tenantOrg.inviteMessage')">
          <t-input v-model="inviteMessage" :maxlength="500" />
        </t-form-item>
      </t-form>
      <div v-if="inviteUrl" class="invite-result">
        <t-input :value="inviteUrl" readonly>
          <template #suffix>
            <t-button size="small" variant="text" @click="copyInviteUrl">
              <t-icon name="file-copy" />
            </t-button>
          </template>
        </t-input>
        <p class="invite-hint">{{ $t('tenantOrg.inviteHint') }}</p>
      </div>
    </t-dialog>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { MessagePlugin, DialogPlugin } from 'tdesign-vue-next'
import { useAuthStore } from '@/stores/auth'
import { copyWithToast } from '@/utils/clipboard'
import { fetchAllTenantMembers } from '@/api/tenant/members'
import {
  listOrgs, createOrg, updateOrg, deleteOrg,
  listOrgMembers, addOrgMember, updateOrgMemberRole, removeOrgMember,
  createOrgInviteLink,
  type TenantOrg, type TenantOrgMember, type TenantOrgRole,
} from '@/api/tenant/orgs'
import type { TenantRole } from '@/api/tenant/members'

const { t } = useI18n()
const authStore = useAuthStore()

const orgs = ref<TenantOrg[]>([])
const loading = ref(false)
const saving = ref(false)

// Tenant Admin/Owner manage orgs; org managers only see actions the
// backend will accept (member ops + invite links on their own orgs).
const canManageTenant = computed(() => authStore.canAccessAllTenants || authStore.hasRole('admin'))
const managedOrgIds = ref<Set<number>>(new Set())

const orgDialog = ref(false)
const editingOrg = ref<TenantOrg | null>(null)
const orgForm = ref({ name: '', description: '' })

const membersDrawer = ref(false)
const activeOrg = ref<TenantOrg | null>(null)
const members = ref<TenantOrgMember[]>([])
const tenantMembers = ref<Array<{ user_id: string; username: string; email: string }>>([])
const newMemberId = ref('')
const newMemberRole = ref<TenantOrgRole>('member')

const inviteDialog = ref(false)
const inviteRole = ref<TenantRole>('viewer')
const inviteMessage = ref('')
const inviteUrl = ref('')

const membersTitle = computed(() =>
  activeOrg.value ? t('tenantOrg.membersTitle', { org: activeOrg.value.name }) : '')
const memberCandidates = computed(() =>
  tenantMembers.value
    .filter((m) => !members.value.some((om) => om.user_id === m.user_id))
    .map((m) => ({ label: `${m.username} (${m.email})`, value: m.user_id })))
const orgRoleOptions = computed(() => [
  { label: t('tenantOrg.role.member'), value: 'member' },
  { label: t('tenantOrg.role.manager'), value: 'manager' },
])
const inviteRoleOptions = computed(() => [
  { label: t('tenantMember.role.viewer'), value: 'viewer' },
  { label: t('tenantMember.role.contributor'), value: 'contributor' },
])

const orgColumns = computed(() => [
  { colKey: 'name', title: t('tenantOrg.nameLabel'), cell: 'name' },
  { colKey: 'description', title: t('tenantOrg.descriptionLabel'), ellipsis: true },
  { colKey: 'actions', title: t('tenantMember.columns.operations'), cell: 'actions', width: 260 },
])
const memberColumns = computed(() => [
  { colKey: 'username', title: t('tenantMember.columns.member'), ellipsis: true,
    cell: (h: any, { row }: any) => row.username || row.email || row.user_id },
  { colKey: 'role', title: t('tenantOrg.roleLabel'), cell: 'role', width: 100 },
  { colKey: 'memberActions', title: t('tenantMember.columns.operations'), cell: 'memberActions', width: 170 },
])

const loadOrgs = async () => {
  loading.value = true
  try {
    const res: any = await listOrgs()
    orgs.value = res?.data || []
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('tenantOrg.loadFailed'))
  } finally {
    loading.value = false
  }
}

const openCreateDialog = () => {
  editingOrg.value = null
  orgForm.value = { name: '', description: '' }
  orgDialog.value = true
}
const openEdit = (org: TenantOrg) => {
  editingOrg.value = org
  orgForm.value = { name: org.name, description: org.description || '' }
  orgDialog.value = true
}
const saveOrg = async () => {
  if (!orgForm.value.name.trim()) return
  saving.value = true
  try {
    if (editingOrg.value) {
      await updateOrg(editingOrg.value.id, orgForm.value)
    } else {
      await createOrg(orgForm.value)
    }
    orgDialog.value = false
    await loadOrgs()
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('tenantOrg.saveFailed'))
  } finally {
    saving.value = false
  }
}
const confirmDelete = (org: TenantOrg) => {
  const d = DialogPlugin.confirm({
    header: t('common.confirm'),
    body: t('tenantOrg.deleteConfirm', { name: org.name }),
    onConfirm: async () => {
      d.destroy()
      try {
        await deleteOrg(org.id)
        await loadOrgs()
      } catch (e: any) {
        MessagePlugin.error(e?.message || t('tenantOrg.deleteFailed'))
      }
    },
    onCancel: () => d.destroy(),
  })
}

const openMembers = async (org: TenantOrg) => {
  activeOrg.value = org
  membersDrawer.value = true
  newMemberId.value = ''
  const tenantId = Number(authStore.currentTenantId || 0)
  const [mRes, tmRes]: any[] = await Promise.all([
    listOrgMembers(org.id),
    tenantId ? fetchAllTenantMembers(tenantId) : Promise.resolve([]),
  ])
  members.value = mRes?.data || []
  tenantMembers.value = tmRes || []
}
const addMember = async () => {
  if (!activeOrg.value || !newMemberId.value) return
  saving.value = true
  try {
    await addOrgMember(activeOrg.value.id, { user_id: newMemberId.value, role: newMemberRole.value })
    newMemberId.value = ''
    const res: any = await listOrgMembers(activeOrg.value.id)
    members.value = res?.data || []
    await loadOrgs()
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('tenantOrg.saveFailed'))
  } finally {
    saving.value = false
  }
}
const setRole = async (row: TenantOrgMember, role: TenantOrgRole) => {
  if (!activeOrg.value) return
  try {
    await updateOrgMemberRole(activeOrg.value.id, row.user_id, { role })
    const res: any = await listOrgMembers(activeOrg.value.id)
    members.value = res?.data || []
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('tenantOrg.saveFailed'))
  }
}
const promote = (row: TenantOrgMember) => setRole(row, 'manager')
const demote = (row: TenantOrgMember) => setRole(row, 'member')
const removeMember = async (row: TenantOrgMember) => {
  if (!activeOrg.value) return
  try {
    await removeOrgMember(activeOrg.value.id, row.user_id)
    const res: any = await listOrgMembers(activeOrg.value.id)
    members.value = res?.data || []
    await loadOrgs()
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('tenantOrg.saveFailed'))
  }
}

const openInvite = (org: TenantOrg) => {
  activeOrg.value = org
  inviteDialog.value = true
  inviteUrl.value = ''
  inviteMessage.value = ''
}
const generateInvite = async () => {
  if (!activeOrg.value) return
  saving.value = true
  try {
    const res: any = await createOrgInviteLink(activeOrg.value.id, {
      role: inviteRole.value,
      message: inviteMessage.value || undefined,
    })
    inviteUrl.value = res?.data?.url || ''
    if (inviteUrl.value && inviteUrl.value.startsWith('/')) {
      inviteUrl.value = window.location.origin + inviteUrl.value
    }
  } catch (e: any) {
    MessagePlugin.error(e?.message || t('tenantOrg.saveFailed'))
  } finally {
    saving.value = false
  }
}
const copyInviteUrl = () => {
  if (inviteUrl.value) copyWithToast(inviteUrl.value, 'tenantOrg.linkCopied')
}

onMounted(loadOrgs)
</script>

<style scoped lang="less">
.tenant-orgs {
  .section-header {
    margin-bottom: 16px;
  }
  .section-header-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }
  .section-description {
    color: var(--td-text-color-secondary);
    font-size: 13px;
    margin-top: 4px;
  }
  .org-name-cell {
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .org-name {
    font-weight: 500;
  }
  .org-member-count {
    font-size: 12px;
    color: var(--td-text-color-secondary);
  }
  .empty-state {
    padding: 32px 0;
    text-align: center;
    color: var(--td-text-color-secondary);
  }
}
.members-pane {
  .members-toolbar {
    display: flex;
    gap: 8px;
    margin-bottom: 12px;
    .member-select {
      flex: 1;
    }
    .role-select {
      width: 120px;
    }
  }
}
.invite-result {
  margin-top: 12px;
}
.invite-hint {
  margin-top: 8px;
  font-size: 12px;
  color: var(--td-text-color-secondary);
}
</style>
