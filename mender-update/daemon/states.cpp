// Copyright 2023 Northern.tech AS
//
//    Licensed under the Apache License, Version 2.0 (the "License");
//    you may not use this file except in compliance with the License.
//    You may obtain a copy of the License at
//
//        http://www.apache.org/licenses/LICENSE-2.0
//
//    Unless required by applicable law or agreed to in writing, software
//    distributed under the License is distributed on an "AS IS" BASIS,
//    WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//    See the License for the specific language governing permissions and
//    limitations under the License.

#include <mender-update/daemon/states.hpp>

#include <common/conf.hpp>
#include <common/events_io.hpp>
#include <common/log.hpp>

#include <mender-update/daemon/context.hpp>
#include <mender-update/deployments.hpp>

namespace mender {
namespace update {
namespace daemon {

namespace conf = mender::common::conf;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace log = mender::common::log;
namespace kv_db = mender::common::key_value_database;

class DefaultStateHandler {
public:
	void operator()(const error::Error &err) {
		if (err != error::NoError) {
			log::Error(err.String());
			poster.PostEvent(StateEvent::Failure);
			return;
		}
		poster.PostEvent(StateEvent::Success);
	}

	sm::EventPoster<StateEvent> &poster;
};

static void DefaultAsyncErrorHandler(sm::EventPoster<StateEvent> &poster, const error::Error &err) {
	if (err != error::NoError) {
		poster.PostEvent(StateEvent::Failure);
	}
}

void IdleState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Entering Idle state");
}

void SubmitInventoryState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Submitting inventory");

	// Schedule timer for next update first, so that long running submissions do not postpone
	// the schedule.
	poll_timer_.AsyncWait(
		chrono::seconds(ctx.mender_context.GetConfig().inventory_poll_interval_seconds),
		[&poster](error::Error err) {
			if (err != error::NoError) {
				log::Error("Inventory poll timer caused error: " + err.String());
			} else {
				poster.PostEvent(StateEvent::InventoryPollingTriggered);
			}
		});

	// TODO: MEN-6576
	poster.PostEvent(StateEvent::Success);
}

void PollForDeploymentState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Polling for update");

	// Schedule timer for next update first, so that long running submissions do not postpone
	// the schedule.
	poll_timer_.AsyncWait(
		chrono::seconds(ctx.mender_context.GetConfig().update_poll_interval_seconds),
		[&poster](error::Error err) {
			if (err != error::NoError) {
				log::Error("Update poll timer caused error: " + err.String());
			} else {
				poster.PostEvent(StateEvent::DeploymentPollingTriggered);
			}
		});

	auto err = mender::update::deployments::CheckNewDeployments(
		ctx.mender_context,
		ctx.mender_context.GetConfig().server_url,
		ctx.deployment_client,
		[&ctx, &poster](mender::update::deployments::CheckUpdatesAPIResponse response) {
			if (!response) {
				log::Error("Error while polling for deployment: " + response.error().String());
				poster.PostEvent(StateEvent::Failure);
				return;
			} else if (!response.value()) {
				log::Info("No update available");
				poster.PostEvent(StateEvent::NothingToDo);
				return;
			}

			auto exp_data = ApiResponseJsonToStateData(response.value().value());
			if (!exp_data) {
				log::Error("Error in API response: " + exp_data.error().String());
				poster.PostEvent(StateEvent::Failure);
				return;
			}

			// Make a new set of update data.
			ctx.deployment.state_data.reset(new StateData(std::move(exp_data.value())));

			log::Info(
				"Deployment with ID " + ctx.deployment.state_data->update_info.id + " started.");

			poster.PostEvent(StateEvent::Success);
		});

	if (err != error::NoError) {
		log::Error("Error when trying to poll for deployment: " + err.String());
		poster.PostEvent(StateEvent::Failure);
	}
}

void UpdateDownloadState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Entering Download state");

	auto req = make_shared<http::OutgoingRequest>();
	req->SetMethod(http::Method::GET);
	auto err = req->SetAddress(ctx.deployment.state_data->update_info.artifact.source.uri);
	if (err != error::NoError) {
		log::Error(err.String());
		poster.PostEvent(StateEvent::Failure);
		return;
	}

	err = ctx.download_client.AsyncCall(
		req,
		[&ctx, &poster](http::ExpectedIncomingResponsePtr exp_resp) {
			if (!exp_resp) {
				log::Error(exp_resp.error().String());
				poster.PostEvent(StateEvent::Failure);
				return;
			}

			auto &resp = exp_resp.value();
			if (resp->GetStatusCode() != http::StatusOK) {
				log::Error(
					"Unexpected status code while fetching artifact: " + resp->GetStatusMessage());
				ctx.download_client.Cancel();
				poster.PostEvent(StateEvent::Failure);
				return;
			}

			auto http_reader = resp->MakeBodyAsyncReader();
			ctx.deployment.artifact_reader =
				make_shared<events::io::ReaderFromAsyncReader>(ctx.event_loop, http_reader);
			ParseArtifact(ctx, poster);
		},
		[&poster](http::ExpectedIncomingResponsePtr exp_resp) {
			if (!exp_resp) {
				log::Error(exp_resp.error().String());
				poster.PostEvent(StateEvent::Failure);
				return;
			}
		});

	if (err != error::NoError) {
		log::Error(err.String());
		poster.PostEvent(StateEvent::Failure);
		return;
	}
}

void UpdateDownloadState::ParseArtifact(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	artifact::config::ParserConfig config {
		conf::paths::DefaultArtScriptsPath,
	};
	auto exp_parser = artifact::Parse(*ctx.deployment.artifact_reader, config);
	if (!exp_parser) {
		log::Error(exp_parser.error().String());
		poster.PostEvent(StateEvent::Failure);
		return;
	}
	ctx.deployment.artifact_parser.reset(new artifact::Artifact(std::move(exp_parser.value())));

	auto exp_header = artifact::View(*ctx.deployment.artifact_parser, 0);
	if (!exp_header) {
		log::Error(exp_header.error().String());
		poster.PostEvent(StateEvent::Failure);
		return;
	}
	auto &header = exp_header.value();

	log::Info("Installing artifact...");

	ctx.deployment.state_data->FillUpdateDataFromArtifact(header);

	if (header.header.payload_type == "") {
		// Empty-payload-artifact, aka "bootstrap artifact".
		poster.PostEvent(StateEvent::NothingToDo);
		return;
	}

	ctx.deployment.update_module.reset(
		new update_module::UpdateModule(ctx.mender_context, header.header.payload_type));

	auto err = ctx.deployment.update_module->CleanAndPrepareFileTree(
		ctx.deployment.update_module->GetUpdateModuleWorkDir(), header);
	if (err != error::NoError) {
		log::Error(err.String());
		poster.PostEvent(StateEvent::Failure);
		return;
	}

	auto exp_payload = ctx.deployment.artifact_parser->Next();
	if (!exp_payload) {
		log::Error(exp_payload.error().String());
		poster.PostEvent(StateEvent::Failure);
		return;
	}
	ctx.deployment.artifact_payload.reset(new artifact::Payload(std::move(exp_payload.value())));

	ctx.deployment.update_module->AsyncDownload(
		ctx.event_loop, *ctx.deployment.artifact_payload, [&poster](error::Error err) {
			if (err != error::NoError) {
				log::Error(err.String());
				poster.PostEvent(StateEvent::Failure);
				return;
			}

			poster.PostEvent(StateEvent::Success);
		});
}

void UpdateInstallState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Entering ArtifactInstall state");

	DefaultAsyncErrorHandler(
		poster,
		ctx.deployment.update_module->AsyncArtifactInstall(
			ctx.event_loop, DefaultStateHandler {poster}));
}

void UpdateCheckRebootState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	DefaultAsyncErrorHandler(
		poster,
		ctx.deployment.update_module->AsyncNeedsReboot(
			ctx.event_loop, [&ctx, &poster](update_module::ExpectedRebootAction reboot_action) {
				if (!reboot_action.has_value()) {
					log::Error(reboot_action.error().String());
					poster.PostEvent(StateEvent::Failure);
					return;
				}

				ctx.deployment.state_data->update_info.reboot_requested.resize(1);
				ctx.deployment.state_data->update_info.reboot_requested[0] =
					NeedsRebootToDbString(*reboot_action);
				switch (*reboot_action) {
				case update_module::RebootAction::No:
					poster.PostEvent(StateEvent::NothingToDo);
					break;
				case update_module::RebootAction::Yes:
				case update_module::RebootAction::Automatic:
					poster.PostEvent(StateEvent::Success);
					break;
				}
			}));
}

void UpdateRebootState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Entering ArtifactReboot state");

	DefaultAsyncErrorHandler(
		poster,
		ctx.deployment.update_module->AsyncArtifactReboot(
			ctx.event_loop, DefaultStateHandler {poster}));
}

void UpdateVerifyRebootState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Entering ArtifactVerifyReboot state");

	DefaultAsyncErrorHandler(
		poster,
		ctx.deployment.update_module->AsyncArtifactVerifyReboot(
			ctx.event_loop, DefaultStateHandler {poster}));
}

void UpdateCommitState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Entering ArtifactCommit state");

	DefaultAsyncErrorHandler(
		poster,
		ctx.deployment.update_module->AsyncArtifactCommit(
			ctx.event_loop, DefaultStateHandler {poster}));
}

void UpdateCheckRollbackState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	DefaultAsyncErrorHandler(
		poster,
		ctx.deployment.update_module->AsyncSupportsRollback(
			ctx.event_loop, [&ctx, &poster](expected::ExpectedBool rollback_supported) {
				if (!rollback_supported.has_value()) {
					log::Error(rollback_supported.error().String());
					poster.PostEvent(StateEvent::Failure);
					return;
				}

				ctx.deployment.state_data->update_info.supports_rollback =
					SupportsRollbackToDbString(*rollback_supported);
				if (*rollback_supported) {
					poster.PostEvent(StateEvent::Success);
				} else {
					poster.PostEvent(StateEvent::NothingToDo);
				}
			}));
}

void UpdateRollbackState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Entering ArtifactRollback state");

	DefaultAsyncErrorHandler(
		poster,
		ctx.deployment.update_module->AsyncArtifactRollback(
			ctx.event_loop, DefaultStateHandler {poster}));
}

void UpdateRollbackRebootState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Entering ArtifactRollbackReboot state");

	DefaultAsyncErrorHandler(
		poster,
		ctx.deployment.update_module->AsyncArtifactRollbackReboot(
			ctx.event_loop, DefaultStateHandler {poster}));
}

void UpdateVerifyRollbackRebootState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Entering ArtifactVerifyRollbackReboot state");

	DefaultAsyncErrorHandler(
		poster,
		ctx.deployment.update_module->AsyncArtifactVerifyRollbackReboot(
			ctx.event_loop, DefaultStateHandler {poster}));
}

void UpdateFailureState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Entering ArtifactFailure state");

	DefaultAsyncErrorHandler(
		poster,
		ctx.deployment.update_module->AsyncArtifactFailure(
			ctx.event_loop, DefaultStateHandler {poster}));
}

void UpdateSaveArtifactDataState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	auto &artifact = ctx.deployment.state_data->update_info.artifact;

	auto err = ctx.mender_context.CommitArtifactData(
		artifact.artifact_name,
		artifact.artifact_group,
		artifact.type_info_provides,
		artifact.clears_artifact_provides,
		[](kv_db::Transaction &txn) {
			// TODO: Erase State Data.
			return error::NoError;
		});
	if (err != error::NoError) {
		log::Error("Error saving artifact data: " + err.String());
		poster.PostEvent(StateEvent::Failure);
		return;
	}

	poster.PostEvent(StateEvent::Success);
}

void UpdateCleanupState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Entering ArtifactCleanup state");

	DefaultAsyncErrorHandler(
		poster,
		ctx.deployment.update_module->AsyncArtifactReboot(
			ctx.event_loop, [&ctx, &poster](error::Error err) {
				DefaultStateHandler handler {poster};
				handler(err);

				ctx.deployment = {};
			}));
}

} // namespace daemon
} // namespace update
} // namespace mender
