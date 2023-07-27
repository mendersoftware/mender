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

namespace mender {
namespace update {
namespace daemon {

namespace conf = mender::common::conf;
namespace error = mender::common::error;
namespace events = mender::common::events;
namespace log = mender::common::log;
namespace kv_db = mender::common::key_value_database;

namespace main_context = mender::update::context;

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
		log::Error(err.String());
		poster.PostEvent(StateEvent::Failure);
	}
}

void EmptyState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	// Keep this state truly empty.
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

	auto err = ctx.deployment_client->CheckNewDeployments(
		ctx.mender_context,
		ctx.mender_context.GetConfig().server_url,
		ctx.http_client,
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

			poster.PostEvent(StateEvent::DeploymentStarted);
			poster.PostEvent(StateEvent::Success);
		});

	if (err != error::NoError) {
		log::Error("Error when trying to poll for deployment: " + err.String());
		poster.PostEvent(StateEvent::Failure);
	}
}

void SaveState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	assert(ctx.deployment.state_data);

	ctx.deployment.state_data->state = DatabaseStateString();

	auto err = ctx.SaveDeploymentStateData(*ctx.deployment.state_data);
	if (err != error::NoError) {
		log::Error(err.String());
		if (err.code
			== main_context::MakeError(main_context::StateDataStoreCountExceededError, "").code) {
			poster.PostEvent(StateEvent::StateLoopDetected);
			return;
		} else if (!IsFailureState()) {
			// Non-failure states should be interrupted, but failure states should be
			// allowed to do their work, even if a database error was detected.
			poster.PostEvent(StateEvent::Failure);
			return;
		}
	}

	OnEnterSaveState(ctx, poster);
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
		[](http::ExpectedIncomingResponsePtr exp_resp) {
			if (!exp_resp) {
				log::Error(exp_resp.error().String());
				// Cannot handle error here, because this handler is called at the
				// end of the download, when we have already left this state. So
				// rely on this error being propagated through the BodyAsyncReader
				// above instead.
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

	ctx.deployment.state_data->state = Context::kUpdateStateDownload;

	assert(ctx.deployment.state_data->update_info.artifact.payload_types.size() == 1);

	// Initial state data save, now that we have enough information from the artifact.
	auto err = ctx.SaveDeploymentStateData(*ctx.deployment.state_data);
	if (err != error::NoError) {
		log::Error(err.String());
		if (err.code
			== main_context::MakeError(main_context::StateDataStoreCountExceededError, "").code) {
			poster.PostEvent(StateEvent::StateLoopDetected);
			return;
		} else {
			poster.PostEvent(StateEvent::Failure);
			return;
		}
	}

	if (header.header.payload_type == "") {
		// Empty-payload-artifact, aka "bootstrap artifact".
		poster.PostEvent(StateEvent::NothingToDo);
		return;
	}

	ctx.deployment.update_module.reset(
		new update_module::UpdateModule(ctx.mender_context, header.header.payload_type));

	err = ctx.deployment.update_module->CleanAndPrepareFileTree(
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

SendStatusUpdateState::SendStatusUpdateState(
	optional::optional<deployments::DeploymentStatus> status, FailureMode mode) :
	status_(status),
	mode_(mode) {
}

void SendStatusUpdateState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	assert(ctx.deployment_client);
	assert(ctx.deployment.state_data);

	auto result_handler = [this, &poster](const error::Error &err) {
		if (err != error::NoError) {
			log::Error("Could not send deployment status: " + err.String());
			switch (mode_) {
			case FailureMode::Ignore:
				break;
			case FailureMode::Fail:
			case FailureMode::RetryThenFail: // MEN-6573: Handle this
				poster.PostEvent(StateEvent::Failure);
				return;
			}
		}

		poster.PostEvent(StateEvent::Success);
	};

	deployments::DeploymentStatus status;
	if (status_) {
		status = status_.value();
	} else {
		// If nothing is specified, grab success/failure status from the deployment status.
		if (ctx.deployment.failed) {
			status = deployments::DeploymentStatus::Failure;
		} else {
			status = deployments::DeploymentStatus::Success;
		}
	}

	auto err = ctx.deployment_client->PushStatus(
		ctx.deployment.state_data->update_info.id,
		status,
		"",
		ctx.mender_context.GetConfig().server_url,
		ctx.http_client,
		result_handler);

	if (err != error::NoError) {
		result_handler(err);
	}

	// No action, wait for reply from status endpoint.
}

void UpdateInstallState::OnEnterSaveState(Context &ctx, sm::EventPoster<StateEvent> &poster) {
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

void UpdateRebootState::OnEnterSaveState(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Entering ArtifactReboot state");

	assert(ctx.deployment.state_data->update_info.reboot_requested.size() == 1);
	auto exp_reboot_mode =
		DbStringToNeedsReboot(ctx.deployment.state_data->update_info.reboot_requested[0]);
	// Should always be true because we check it at load time.
	assert(exp_reboot_mode);

	switch (exp_reboot_mode.value()) {
	case update_module::RebootAction::No:
		// Should not happen because then we don't enter this state.
		assert(false);
		poster.PostEvent(StateEvent::Failure);
		break;
	case update_module::RebootAction::Yes:
		DefaultAsyncErrorHandler(
			poster,
			ctx.deployment.update_module->AsyncArtifactReboot(
				ctx.event_loop, DefaultStateHandler {poster}));
		break;
	case update_module::RebootAction::Automatic:
		DefaultAsyncErrorHandler(
			poster,
			ctx.deployment.update_module->AsyncSystemReboot(
				ctx.event_loop, DefaultStateHandler {poster}));
		break;
	}
}

void UpdateVerifyRebootState::OnEnterSaveState(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Entering ArtifactVerifyReboot state");

	DefaultAsyncErrorHandler(
		poster,
		ctx.deployment.update_module->AsyncArtifactVerifyReboot(
			ctx.event_loop, DefaultStateHandler {poster}));
}

void UpdateCommitState::OnEnterSaveState(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Entering ArtifactCommit state");

	DefaultAsyncErrorHandler(
		poster,
		ctx.deployment.update_module->AsyncArtifactCommit(
			ctx.event_loop, DefaultStateHandler {poster}));
}

void UpdateAfterCommitState::OnEnterSaveState(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	// TODO: Will need to run ArtifactCommit_Leave scripts in here. Maybe it should be renamed
	// to something with state scripts also.

	// Now we have committed. If we had a schema update, re-save state data with the new schema.
	assert(ctx.deployment.state_data);
	auto &state_data = *ctx.deployment.state_data;
	if (state_data.update_info.has_db_schema_update) {
		state_data.update_info.has_db_schema_update = false;
		auto err = ctx.SaveDeploymentStateData(state_data);
		if (err != error::NoError) {
			log::Error("Not able to commit schema update: " + err.String());
			poster.PostEvent(StateEvent::Failure);
			return;
		}
	}

	poster.PostEvent(StateEvent::Success);
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
					poster.PostEvent(StateEvent::RollbackStarted);
					poster.PostEvent(StateEvent::Success);
				} else {
					poster.PostEvent(StateEvent::NothingToDo);
				}
			}));
}

void UpdateRollbackState::OnEnterSaveState(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Entering ArtifactRollback state");

	DefaultAsyncErrorHandler(
		poster,
		ctx.deployment.update_module->AsyncArtifactRollback(
			ctx.event_loop, DefaultStateHandler {poster}));
}

void UpdateRollbackRebootState::OnEnterSaveState(
	Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Entering ArtifactRollbackReboot state");

	// We ignore errors in this state as long as the ArtifactVerifyRollbackReboot state
	// succeeds.
	auto err = ctx.deployment.update_module->AsyncArtifactRollbackReboot(
		ctx.event_loop, [&poster](error::Error err) {
			if (err != error::NoError) {
				log::Error(err.String());
			}
			poster.PostEvent(StateEvent::Success);
		});

	if (err != error::NoError) {
		log::Error(err.String());
		poster.PostEvent(StateEvent::Success);
	}
}

void UpdateVerifyRollbackRebootState::OnEnterSaveState(
	Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Entering ArtifactVerifyRollbackReboot state");

	// In this state we only retry, we don't fail. If this keeps on going forever, then the
	// state loop detection will eventually kick in.
	auto err = ctx.deployment.update_module->AsyncArtifactVerifyRollbackReboot(
		ctx.event_loop, [&poster](error::Error err) {
			if (err != error::NoError) {
				log::Error(err.String());
				poster.PostEvent(StateEvent::Retry);
				return;
			}
			poster.PostEvent(StateEvent::Success);
		});
	if (err != error::NoError) {
		log::Error(err.String());
		poster.PostEvent(StateEvent::Retry);
	}
}

void UpdateRollbackSuccessfulState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	ctx.deployment.state_data->update_info.all_rollbacks_successful = true;
	poster.PostEvent(StateEvent::Success);
}

void UpdateFailureState::OnEnterSaveState(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Entering ArtifactFailure state");

	DefaultAsyncErrorHandler(
		poster,
		ctx.deployment.update_module->AsyncArtifactFailure(
			ctx.event_loop, DefaultStateHandler {poster}));
}

static string AddInconsistentSuffix(const string &str) {
	const auto &suffix = main_context::MenderContext::broken_artifact_name_suffix;
	// `string::ends_with` is C++20... grumble
	string ret {str};
	if (!common::EndsWith(ret, suffix)) {
		ret.append(suffix);
	}
	return ret;
}

void UpdateSaveProvidesState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	if (ctx.deployment.failed && !ctx.deployment.rollback_failed) {
		// If the update failed, but we rolled back successfully, then we don't need to do
		// anything, just keep the old data.
		poster.PostEvent(StateEvent::Success);
		return;
	}

	assert(ctx.deployment.state_data);
	// This state should never happen: rollback failed, but update not failed??
	assert(!(!ctx.deployment.failed && ctx.deployment.rollback_failed));

	// We expect Cleanup to be the next state after this.
	ctx.deployment.state_data->state = ctx.kUpdateStateCleanup;

	auto &artifact = ctx.deployment.state_data->update_info.artifact;

	if (ctx.deployment.rollback_failed) {
		artifact.artifact_name = AddInconsistentSuffix(artifact.artifact_name);
	}

	auto err = ctx.mender_context.CommitArtifactData(
		artifact.artifact_name,
		artifact.artifact_group,
		artifact.type_info_provides,
		artifact.clears_artifact_provides,
		[&ctx](kv_db::Transaction &txn) {
			// Save the Cleanup state together with the artifact data, atomically.
			return ctx.SaveDeploymentStateData(txn, *ctx.deployment.state_data);
		});
	if (err != error::NoError) {
		log::Error("Error saving artifact data: " + err.String());
		if (err.code
			== main_context::MakeError(main_context::StateDataStoreCountExceededError, "").code) {
			poster.PostEvent(StateEvent::StateLoopDetected);
			return;
		}
		poster.PostEvent(StateEvent::Failure);
		return;
	}

	poster.PostEvent(StateEvent::Success);
}

void UpdateCleanupState::OnEnterSaveState(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	log::Debug("Entering ArtifactCleanup state");

	// It's possible for there not to be an initialized update_module structure, if the
	// deployment failed before we could successfully parse the artifact. If so, cleanup is a
	// no-op.
	if (!ctx.deployment.update_module) {
		poster.PostEvent(StateEvent::Success);
		return;
	}

	DefaultAsyncErrorHandler(
		poster,
		ctx.deployment.update_module->AsyncCleanup(ctx.event_loop, DefaultStateHandler {poster}));
}

void ClearArtifactDataState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	auto err = ctx.mender_context.GetMenderStoreDB().WriteTransaction([](kv_db::Transaction &txn) {
		// Remove state data, since we're done now.
		auto err = txn.Remove(main_context::MenderContext::state_data_key);
		if (err != error::NoError) {
			return err;
		}
		return txn.Remove(main_context::MenderContext::state_data_key_uncommitted);
	});
	if (err != error::NoError) {
		log::Error("Error removing artifact data: " + err.String());
		poster.PostEvent(StateEvent::Failure);
		return;
	}

	poster.PostEvent(StateEvent::Success);
}

void StateLoopState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	assert(ctx.deployment.state_data);
	auto &artifact = ctx.deployment.state_data->update_info.artifact;

	// Mark update as inconsistent.
	artifact.artifact_name = AddInconsistentSuffix(artifact.artifact_name);

	auto err = ctx.mender_context.CommitArtifactData(
		artifact.artifact_name,
		artifact.artifact_group,
		artifact.type_info_provides,
		artifact.clears_artifact_provides,
		[](kv_db::Transaction &txn) { return error::NoError; });
	if (err != error::NoError) {
		log::Error("Error saving inconsistent artifact data: " + err.String());
		poster.PostEvent(StateEvent::Failure);
		return;
	}

	poster.PostEvent(StateEvent::Success);
}

void EndOfDeploymentState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	ctx.deployment = {};
	poster.PostEvent(StateEvent::DeploymentEnded);
	poster.PostEvent(StateEvent::Success);
}

ExitState::ExitState(events::EventLoop &event_loop) :
	event_loop_(event_loop) {
}

void ExitState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	event_loop_.Stop();
}

namespace deployment_tracking {

void NoFailuresState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	ctx.deployment.failed = false;
	ctx.deployment.rollback_failed = false;
}

void FailureState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	ctx.deployment.failed = true;
	ctx.deployment.rollback_failed = true;
}

void RollbackAttemptedState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	ctx.deployment.failed = true;
	ctx.deployment.rollback_failed = false;
}

void RollbackFailedState::OnEnter(Context &ctx, sm::EventPoster<StateEvent> &poster) {
	ctx.deployment.failed = true;
	ctx.deployment.rollback_failed = true;
}

} // namespace deployment_tracking

} // namespace daemon
} // namespace update
} // namespace mender
