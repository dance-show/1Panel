<template>
    <el-dialog v-model="open" :title="$t('app.checkTitle')" width="50%" :close-on-click-modal="false">
        <el-row>
            <el-col :span="20" :offset="2" v-if="open">
                <el-alert
                    type="warning"
                    :description="$t('app.deleteHelper', [$t('app.app')])"
                    center
                    show-icon
                    :closable="false"
                />
                <br />
                <el-descriptions border :column="1">
                    <el-descriptions-item v-for="(item, key) in map" :key="key">
                        <template #label>
                            <a href="javascript:void(0);" @click="toPage(item[0])">{{ $t('app.' + item[0]) }}</a>
                        </template>
                        <span class="resources">
                            {{ map.get(item[0]).toString() }}
                        </span>
                    </el-descriptions-item>
                </el-descriptions>
                <div class="center" v-if="installData.key === 'openresty'">
                    <el-checkbox v-model="forceDelete" label="true">{{ $t('app.forceDelete') }}</el-checkbox>
                    <el-alert
                        v-if="forceDelete"
                        type="error"
                        effect="dark"
                        :description="$t('app.openrestyDeleteHelper')"
                        center
                        :closable="false"
                    />
                </div>
            </el-col>
        </el-row>

        <template #footer v-if="forceDelete">
            <span class="dialog-footer">
                <el-button @click="open = false">
                    {{ $t('commons.button.cancel') }}
                </el-button>
                <el-button type="primary" @click="onConfirm">
                    {{ $t('commons.button.confirm') }}
                </el-button>
            </span>
        </template>
    </el-dialog>
</template>
<script lang="ts" setup>
import { App } from '@/api/interface/app';
import { InstalledOp } from '@/api/modules/app';
import i18n from '@/lang';
import { MsgSuccess } from '@/utils/message';
import { ref } from 'vue';
import { useRouter } from 'vue-router';
const router = useRouter();

interface CheckRrops {
    items: App.AppInstallResource[];
    installID: Number;
    key: string;
}
const installData = ref<CheckRrops>({
    items: [],
    installID: 0,
    key: '',
});
const open = ref(false);
const map = new Map();
const forceDelete = ref(false);
const em = defineEmits(['close']);

const acceptParams = (props: CheckRrops) => {
    map.clear();
    forceDelete.value = false;
    installData.value.installID = props.installID;
    installData.value.key = props.key;
    installData.value.items = [];
    installData.value.items = props.items;
    installData.value.items.forEach((item) => {
        if (map.has(item.type)) {
            const array = map.get(item.type);
            array.push(item.name);
            map.set(item.type, array);
        } else {
            map.set(item.type, [item.name]);
        }
    });
    open.value = true;
};

const toPage = (key: string) => {
    if (key === 'app') {
        open.value = false;
    }
    if (key === 'website') {
        router.push({ name: 'Website' });
    }
    if (key === 'database') {
        router.push({ name: 'MySQL' });
    }
};

const onConfirm = () => {
    ElMessageBox.confirm(
        i18n.global.t('app.operatorHelper', [i18n.global.t('app.delete')]),
        i18n.global.t('app.delete'),
        {
            confirmButtonText: i18n.global.t('commons.button.confirm'),
            cancelButtonText: i18n.global.t('commons.button.cancel'),
            type: 'info',
        },
    ).then(() => {
        const deleteReq = {
            operate: 'delete',
            installId: Number(installData.value.installID),
            deleteBackup: true,
            forceDelete: true,
            deleteDB: true,
        };
        InstalledOp(deleteReq).then(() => {
            MsgSuccess(i18n.global.t('commons.msg.operationSuccess'));
            open.value = false;
            em('close', open);
        });
    });
};

defineExpose({
    acceptParams,
});
</script>

<style scoped>
.resources {
    word-break: break-all;
}

.center {
    text-align: center;
}
</style>
